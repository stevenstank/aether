# Aether runtime: process entry and the stack (D-009, D-011, D-022).
#
# _start runs main.main on a stack the runtime maps itself, so no frame
# depends on the process stack limit (ulimit -s). The program declares in
# aether.mainstack how many bytes main.main uses below the call. The stack
# holds S = max(256 MiB, aether.mainstack) bytes, plus .Lreserve below them
# for the runtime helpers, rounded up to whole pages, with a PROT_NONE guard
# page below that. If the stack cannot be allocated, _start prints a fatal
# error and exits with status 2. Otherwise it stores the stack limit L, the
# lowest address a frame may use, in aether.stacklimit, calls main.main, and
# exits with status 0 through exit_group.
#
# Every function's prologue checks its frame against L and jumps to
# aether.stackoverflow if it does not fit (D-011).

	.set	.Lpage, 4096
	.set	.Lreserve, 4096		# helpers use at most 56 bytes below %rsp
	.set	.Ldefault, 268435456	# 256 MiB

	.text
	.globl	_start
	.type	_start, @function
_start:
	xorl	%ebp, %ebp		# outermost frame
	andq	$-16, %rsp
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
	movq	%rax, %rdi
	movl	$.Lpage, %esi
	xorl	%edx, %edx		# PROT_NONE
	movl	$10, %eax		# mprotect: the lowest page is the guard
	syscall
	testq	%rax, %rax
	jnz	1f
	movq	%rbx, %rsp		# 16-byte aligned at the call
	call	main.main
	movl	$231, %eax		# exit_group
	xorl	%edi, %edi		# status 0
	syscall
	hlt				# not reached
1:	leaq	.Lnostack(%rip), %rsi
	movl	$.Lnostacklen, %edx
	call	aether.write
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

	.section	.rodata
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

	.section	.note.GNU-stack,"",@progbits
