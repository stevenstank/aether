# Aether runtime: process entry and the stack (D-009, D-011, D-022).
#
# _start runs main.init and then main.main on a stack the runtime maps
# itself, so no frame depends on the process stack limit (ulimit -s). The
# program declares in aether.mainstack how many bytes the larger of the two
# uses below the call. The stack
# holds S = max(256 MiB, aether.mainstack) bytes, plus .Lreserve below them
# for the runtime helpers, rounded up to whole pages, with a PROT_NONE guard
# page below that. If the stack cannot be allocated, _start prints a fatal
# error and exits with status 2. Otherwise it stores the stack limit L, the
# lowest address a frame may use, in aether.stacklimit, calls main.init,
# which runs package initialization (D-023), then main.main, and exits with
# status 0 through exit_group. Before, it saves the stack's top in
# aether.stacktop and, if the environment has AETHER_GCSTRESS=1, sets
# aether.gcstress, for the collector (heap.s, D-046).
#
# First, as gc's runtime does (checkfds), _start opens /dev/null on each
# of the file descriptors 0, 1, and 2 that is closed, so that writes to
# them succeed (D-049, increment 11.6); if that fails, it prints the fatal
# error "cannot open standard fds" and exits with status 2. It saves argc
# and argv for the system calls aether.argc, aether.argptr, and
# aether.arglen, and envp for aether.envc, aether.envptr, and aether.envlen
# (D-051).
#
# Every function's prologue checks its frame against L and jumps to
# aether.stackoverflow if it does not fit (D-011), or, if it begins with a
# Preempt, against aether.stackguard (preempt.s, D-060).

	.set	.Lpage, 4096
	.set	.Lreserve, 4096		# helpers use at most 256 bytes below %rsp
	.set	.Ldefault, 268435456	# 256 MiB

	.text
	.globl	_start
	.type	_start, @function
_start:
	xorl	%ebp, %ebp		# outermost frame
	xorl	%r12d, %r12d		# as gc's checkfds: a closed fd 0, 1, or 2
5:	movl	%r12d, %edi		# is opened on /dev/null (D-049, 11.6)
	movl	$1, %esi		# F_GETFD
	movl	$72, %eax		# fcntl
	syscall
	cmpq	$-9, %rax		# -EBADF
	jne	6f
	leaq	.Ldevnull(%rip), %rdi
	movl	$2, %esi		# O_RDWR
	xorl	%edx, %edx
	movl	$2, %eax		# open
	syscall
	cmpq	%r12, %rax
	jne	7f
6:	incl	%r12d
	cmpl	$3, %r12d
	jb	5b
	movq	(%rsp), %rax		# argc
	movq	%rax, .Largc(%rip)	# for aether.argc and the others below
	leaq	8(%rsp), %rcx
	movq	%rcx, .Largv(%rip)
	leaq	16(%rsp,%rax,8), %rcx	# envp, after argv and its 0
	movq	%rcx, .Lenvp(%rip)
2:	movq	(%rcx), %rsi		# AETHER_GCSTRESS=1 sets aether.gcstress (D-046)
	testq	%rsi, %rsi
	jz	4f
	addq	$8, %rcx
	leaq	.Lstressvar(%rip), %rdi
3:	movb	(%rdi), %al
	cmpb	%al, (%rsi)
	jne	2b
	incq	%rdi
	incq	%rsi
	testb	%al, %al
	jnz	3b
	movq	$1, aether.gcstress(%rip)
4:	andq	$-16, %rsp
	movq	aether.mainstack(%rip), %rsi
	movl	$.Ldefault, %eax
	cmpq	%rax, %rsi
	cmovbq	%rax, %rsi		# S
	addq	$.Lreserve+.Lpage+.Lpage-1, %rsi
	andq	$-.Lpage, %rsi		# guard page + stack, in whole pages
	xorl	%edi, %edi		# any address
	movl	$3, %edx		# PROT_READ|PROT_WRITE
	movl	$0x20022, %r10d		# MAP_PRIVATE|MAP_ANONYMOUS|MAP_STACK
	movq	$-1, %r8		# no file
	xorl	%r9d, %r9d
	movl	$9, %eax		# mmap
	syscall				# clobbers %rcx and %r11
	cmpq	$-4095, %rax
	jae	1f			# -errno
	leaq	(%rax,%rsi), %rbx	# top of the stack, page-aligned
	leaq	.Lpage+.Lreserve(%rax), %rcx
	movq	%rcx, aether.stacklimit(%rip)	# L, before any call to Aether code
	movq	%rcx, aether.stackguard(%rip)	# and the guard (preempt.s, D-060)
	movq	%rax, %rdi
	movl	$.Lpage, %esi
	xorl	%edx, %edx		# PROT_NONE
	movl	$10, %eax		# mprotect: the lowest page is the guard
	syscall
	testq	%rax, %rax
	jnz	1f
	movq	%rbx, aether.stacktop(%rip)	# the end of the collector's roots
	movq	%rbx, %rsp		# 16-byte aligned at the calls
	call	main.init		# ABI-S0: no register survives
	call	main.main
	movl	$231, %eax		# exit_group
	xorl	%edi, %edi		# status 0
	syscall
	hlt				# not reached
7:	leaq	.Lnofds(%rip), %rsi	# perhaps to a closed fd 2
	movl	$.Lnofdslen, %edx
	jmp	8f
1:	leaq	.Lnostack(%rip), %rsi
	movl	$.Lnostacklen, %edx
8:	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi		# status 2, as for gc's fatal errors
	syscall
	hlt				# not reached
	.size	_start, .-_start

# aether.stackoverflow is entered by a jump from a prologue whose frame does
# not fit, with %rsp at least L - 8. It aligns %rsp (up to 8 bytes) and
# calls aether.write (8 bytes), so it uses at most 24 bytes below L, inside
# the reserve. It writes the fatal error and exits with status 2, even if
# the write fails.
	.globl	aether.stackoverflow
	.type	aether.stackoverflow, @function
aether.stackoverflow:
	andq	$-16, %rsp
	leaq	.Loverflow(%rip), %rsi
	movl	$.Loverflowlen, %edx
	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.stackoverflow, .-aether.stackoverflow

# aether.panicdivide, aether.panicshift, and aether.panicnil end the program
# with a run-time error (D-023, D-035): they write "panic: runtime error: "
# and the message, and
# exit with status 2, even if the write fails. They are called from a
# function body, where %rsp is at least L, and use 16 bytes below it: their
# return address and aether.write's (D-011 reserve).
	.globl	aether.panicdivide
	.type	aether.panicdivide, @function
aether.panicdivide:
	leaq	.Lpanicdivide(%rip), %rsi
	movl	$.Lpanicdividelen, %edx
	jmp	.Lpanic
	.size	aether.panicdivide, .-aether.panicdivide

	.globl	aether.panicnil
	.type	aether.panicnil, @function
aether.panicnil:
	leaq	.Lpanicnil(%rip), %rsi
	movl	$.Lpanicnillen, %edx
	jmp	.Lpanic
	.size	aether.panicnil, .-aether.panicnil

	.globl	aether.panicshift
	.type	aether.panicshift, @function
aether.panicshift:
	leaq	.Lpanicshift(%rip), %rsi
	movl	$.Lpanicshiftlen, %edx
.Lpanic:
	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.panicshift, .-aether.panicshift

# aether.panicindex and aether.panicindexu end the program with an index out
# of range (D-033): the index in %rdi, signed or unsigned, and the length in
# %rsi. They write "panic: runtime error: index out of range [", the index,
# and "] with length " and the length, or for a negative index only "]",
# then a newline, as gc does, and exit with status 2. They use at most 72
# bytes below %rsp: their return address, 8 bytes of alignment, and
# aether.printint's 56.
	.globl	aether.panicindex
	.type	aether.panicindex, @function
aether.panicindex:
	movl	$1, %r13d		# signed
	jmp	1f
	.size	aether.panicindex, .-aether.panicindex

	.globl	aether.panicindexu
	.type	aether.panicindexu, @function
aether.panicindexu:
	xorl	%r13d, %r13d
1:	subq	$8, %rsp		# align for the helpers' calls
	movq	%rdi, %rbx		# the helpers preserve %rbx, %r12, %r13
	movq	%rsi, %r12
	leaq	.Lpanicindex(%rip), %rsi
	movl	$.Lpanicindexlen, %edx
	call	aether.write
	movq	%rbx, %rdi
	testl	%r13d, %r13d
	jz	2f
	call	aether.printint
	testq	%rbx, %rbx
	jns	3f
	leaq	.Lpanicclose(%rip), %rsi	# a negative index: no length
	movl	$1, %edx
	call	aether.write
	jmp	4f
2:	call	aether.printuint
3:	leaq	.Lpaniclength(%rip), %rsi
	movl	$.Lpaniclengthlen, %edx
	call	aether.write
	movq	%r12, %rdi
	call	aether.printint
4:	call	aether.printnl
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.panicindexu, .-aether.panicindexu

# aether.panicexit ends the program with exit status 2 after the panics have
# been written (D-043). It writes nothing.
	.globl	aether.panicexit
	.type	aether.panicexit, @function
aether.panicexit:
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.panicexit, .-aether.panicexit

# aether.panicmsg ends the program with a panic whose message is made at
# run time (D-041): the %rsi bytes at %rdi. It writes "panic: ", the message,
# and a newline, and exits with status 2. It uses at most 24 bytes below
# %rsp: its return address, 8 bytes of alignment, and aether.write's.
# aether.panicfatal is the same with "fatal error: " for gc's fatal errors
# (D-048).
	.globl	aether.panicfatal
	.type	aether.panicfatal, @function
aether.panicfatal:
	leaq	.Lfatalmsg(%rip), %rax
	movl	$.Lfatalmsglen, %ecx
	jmp	1f
	.size	aether.panicfatal, .-aether.panicfatal

	.globl	aether.panicmsg
	.type	aether.panicmsg, @function
aether.panicmsg:
	leaq	.Lpanicmsg(%rip), %rax
	movl	$.Lpanicmsglen, %ecx
1:	subq	$8, %rsp		# align for the helpers' calls
	movq	%rdi, %rbx		# the helpers preserve %rbx and %r12
	movq	%rsi, %r12
	movq	%rax, %rsi
	movq	%rcx, %rdx
	call	aether.write
	movq	%rbx, %rsi
	movq	%r12, %rdx
	call	aether.write
	call	aether.printnl
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.panicmsg, .-aether.panicmsg

# The system calls of ir.Sys (D-049, increment 11.6), under the helper
# interface, with their args in %rdi, %rsi, and %rdx and their value in %rax:
#
#   - aether.exit ends the program with exit status %rdi (modulo 256);
#   - aether.syswrite makes one write system call of %rdx bytes at %rsi to
#     file descriptor %rdi and returns its result, the bytes written or the
#     negated errno;
#   - aether.sysread makes one read system call of at most %rdx bytes into
#     %rsi from file descriptor %rdi and returns its result, the bytes read
#     (0 at end of file) or the negated errno;
#   - aether.argc returns the number of the program's arguments, from
#     _start's argc;
#   - aether.argptr and aether.arglen return the address and length of
#     argument %rdi, which is below that number;
#   - aether.envc, aether.envptr, and aether.envlen are the same for the
#     environment strings, from _start's envp (D-051).
#
# They use no stack. aether.syscall6 (D-051) takes its seven args, each a
# word, in the caller's outgoing area, 8(%rsp) to 56(%rsp) after the call:
# the system call's number, then its six arguments. It makes the system
# call and returns its result in %rax, the negated errno on failure. It
# clobbers %rdi, %rsi, %rdx, %r8, %r9, %r10, %rcx, and %r11.
	.globl	aether.exit
	.type	aether.exit, @function
aether.exit:
	movl	$231, %eax		# exit_group
	syscall
	hlt				# not reached
	.size	aether.exit, .-aether.exit

	.globl	aether.syswrite
	.type	aether.syswrite, @function
aether.syswrite:
	movl	$1, %eax		# write
	syscall				# clobbers %rcx and %r11
	ret
	.size	aether.syswrite, .-aether.syswrite

	.globl	aether.sysread
	.type	aether.sysread, @function
aether.sysread:
	xorl	%eax, %eax		# read
	syscall				# clobbers %rcx and %r11
	ret
	.size	aether.sysread, .-aether.sysread

	.globl	aether.syscall6
	.type	aether.syscall6, @function
aether.syscall6:
	movq	8(%rsp), %rax
	movq	16(%rsp), %rdi
	movq	24(%rsp), %rsi
	movq	32(%rsp), %rdx
	movq	40(%rsp), %r10
	movq	48(%rsp), %r8
	movq	56(%rsp), %r9
	syscall				# clobbers %rcx and %r11
	ret
	.size	aether.syscall6, .-aether.syscall6

# aether.cpufma (D-053) returns 1 if the processor and the kernel provide
# AVX and FMA, as gc's internal/cpu decides: CPUID leaf 1's ECX has FMA
# (bit 12), OSXSAVE (bit 27), and AVX (bit 28), and XGETBV's XCR0 has the
# SSE and AVX states (bits 1 and 2). Else 0. It saves %rbx, which CPUID
# writes, in %r11.
	.globl	aether.cpufma
	.type	aether.cpufma, @function
aether.cpufma:
	movq	%rbx, %r11
	movl	$1, %eax
	xorl	%ecx, %ecx
	cpuid
	movq	%r11, %rbx
	movl	%ecx, %r8d
	andl	$0x18001000, %r8d	# FMA, OSXSAVE, AVX
	cmpl	$0x18001000, %r8d
	jne	1f
	xorl	%ecx, %ecx
	xgetbv
	andl	$6, %eax
	cmpl	$6, %eax
	jne	1f
	movl	$1, %eax
	ret
1:	xorl	%eax, %eax
	ret
	.size	aether.cpufma, .-aether.cpufma

# aether.rand (D-053) returns a random word, as gc's runtime.rand: the
# wyrand generator, whose state the getrandom system call seeds at the
# first call (made odd, so that it is never zero, should getrandom fail).
	.globl	aether.rand
	.type	aether.rand, @function
aether.rand:
	movq	.Lrandstate(%rip), %rax
	testq	%rax, %rax
	jne	1f
	leaq	.Lrandstate(%rip), %rdi
	movl	$8, %esi
	xorl	%edx, %edx
	movl	$318, %eax		# getrandom
	syscall
	orq	$1, .Lrandstate(%rip)
	movq	.Lrandstate(%rip), %rax
1:	movabsq	$0xa0761d6478bd642f, %rcx
	addq	%rcx, %rax
	movq	%rax, .Lrandstate(%rip)
	movabsq	$0xe7037ed1a0b428db, %rdx
	xorq	%rax, %rdx
	mulq	%rdx
	xorq	%rdx, %rax
	ret
	.size	aether.rand, .-aether.rand

# aether.cputicks (D-058) returns the time-stamp counter, as gc's
# runtime.cputicks: RDTSC's %edx:%eax.
	.globl	aether.cputicks
	.type	aether.cputicks, @function
aether.cputicks:
	rdtsc
	shlq	$32, %rdx
	orq	%rdx, %rax
	ret
	.size	aether.cputicks, .-aether.cputicks

	.globl	aether.envc
	.type	aether.envc, @function
aether.envc:
	movq	.Lenvp(%rip), %rcx
	xorl	%eax, %eax
1:	cmpq	$0, (%rcx,%rax,8)
	je	2f
	incq	%rax
	jmp	1b
2:	ret
	.size	aether.envc, .-aether.envc

	.globl	aether.envptr
	.type	aether.envptr, @function
aether.envptr:
	movq	.Lenvp(%rip), %rax
	movq	(%rax,%rdi,8), %rax
	ret
	.size	aether.envptr, .-aether.envptr

	.globl	aether.envlen
	.type	aether.envlen, @function
aether.envlen:
	movq	.Lenvp(%rip), %rax
	movq	(%rax,%rdi,8), %rcx
	xorl	%eax, %eax
1:	cmpb	$0, (%rcx,%rax)
	je	2f
	incq	%rax
	jmp	1b
2:	ret
	.size	aether.envlen, .-aether.envlen

	.globl	aether.argc
	.type	aether.argc, @function
aether.argc:
	movq	.Largc(%rip), %rax
	ret
	.size	aether.argc, .-aether.argc

	.globl	aether.argptr
	.type	aether.argptr, @function
aether.argptr:
	movq	.Largv(%rip), %rax
	movq	(%rax,%rdi,8), %rax
	ret
	.size	aether.argptr, .-aether.argptr

	.globl	aether.arglen
	.type	aether.arglen, @function
aether.arglen:
	movq	.Largv(%rip), %rax
	movq	(%rax,%rdi,8), %rcx
	xorl	%eax, %eax
1:	cmpb	$0, (%rcx,%rax)
	je	2f
	incq	%rax
	jmp	1b
2:	ret
	.size	aether.arglen, .-aether.arglen

	.section	.rodata
.Lpanicmsg:
	.ascii	"panic: "
	.set	.Lpanicmsglen, .-.Lpanicmsg
.Lfatalmsg:
	.ascii	"fatal error: "
	.set	.Lfatalmsglen, .-.Lfatalmsg
.Lpanicindex:
	.ascii	"panic: runtime error: index out of range ["
	.set	.Lpanicindexlen, .-.Lpanicindex
.Lpaniclength:
	.ascii	"] with length "
	.set	.Lpaniclengthlen, .-.Lpaniclength
.Lpanicclose:
	.ascii	"]"
.Lpanicdivide:
	.ascii	"panic: runtime error: integer divide by zero\n"
	.set	.Lpanicdividelen, .-.Lpanicdivide
.Lpanicshift:
	.ascii	"panic: runtime error: negative shift amount\n"
	.set	.Lpanicshiftlen, .-.Lpanicshift
.Lpanicnil:
	.ascii	"panic: runtime error: invalid memory address or nil pointer dereference\n"
	.set	.Lpanicnillen, .-.Lpanicnil
.Lstressvar:
	.asciz	"AETHER_GCSTRESS=1"
.Lnofds:
	.ascii	"fatal error: cannot open standard fds\n"
	.set	.Lnofdslen, .-.Lnofds
.Ldevnull:
	.asciz	"/dev/null"
.Lnostack:
	.ascii	"fatal error: cannot allocate the main stack\n"
	.set	.Lnostacklen, .-.Lnostack
.Loverflow:
	.ascii	"fatal error: stack overflow\n"
	.set	.Loverflowlen, .-.Loverflow

	.bss
	.p2align	3
	.globl	aether.stacklimit
	.type	aether.stacklimit, @object
	.size	aether.stacklimit, 8
aether.stacklimit:
	.zero	8
.Largc:
	.zero	8
.Largv:
	.zero	8
.Lenvp:
	.zero	8
.Lrandstate:
	.zero	8

	.section	.note.GNU-stack,"",@progbits
