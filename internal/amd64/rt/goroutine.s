# Aether runtime: goroutine stacks and the context switch (D-048).
#
# All goroutines run on one thread and switch only through aether.goswitch
# and aether.goexit. The main goroutine (handle 0) runs main.init and
# main.main on the stack _start maps (D-009). Every other goroutine runs in
# a 64 MiB slot of one 8 TiB region, reserved with MAP_NORESERVE by the
# first aether.gonew, so there are .Lnslots slots. The top 64 bytes of a
# slot are the goroutine's record, whose address is its handle and the top
# of its stack:
#
#	0(rec)	the saved %rsp, while the goroutine is not running
#	8(rec)	1 while the goroutine is live, 0 once its slot is free
#	16(rec)	the next record on the free list
#	24(rec)	its stack limit L: the slot's base plus the D-011 reserve
#	32(rec)	its stack top: rec itself
#
# The main goroutine's record is aether.g0, in .bss. aether.gcur is the
# running goroutine's handle. aether.stacklimit and aether.stacktop always
# describe the running goroutine's stack: a switch saves them in the
# record it leaves and loads them from the one it resumes.
#
# A goroutine that is not running has pushed %rbx, %rbp, and %r12-%r15 on
# its own stack below its frames, so everything it holds lies in [its saved
# %rsp, its top), which the collector scans (heap.s, .Lscangs). A slot is
# freed only after its goroutine has left it for good: its pages below the
# top page are released with MADV_DONTNEED, and it is reused last in,
# first out. There are no guard pages between slots: the prologue check
# detects overflow (D-009, D-011).
#
# The three helpers follow the helper interface: they preserve %rbx, %rbp,
# and %r12-%r15, require %rsp 16-byte aligned at the call, and use at most
# 64 bytes below %rsp.

	.set	.Lregion, 0x80000000000	# 8 TiB
	.set	.Lslot, 0x4000000	# 64 MiB
	.set	.Lnslots, 131072	# .Lregion / .Lslot
	.set	.Lrec, 64
	.set	.Lreserve, 4096
	.set	.Lpage, 4096

	.text
# aether.gonew returns in %rax the handle of a new goroutine that will run
# the function whose code address is in %rdi, with no arguments, when it is
# first resumed. Its stack starts as if it had switched away just before
# .Lgostart: six register words, the second of them (%rbx) the function,
# and the return address .Lgostart.
	.globl	aether.gonew
	.type	aether.gonew, @function
aether.gonew:
	cmpq	$0, aether.gregion(%rip)
	jne	1f
	pushq	%rdi
	xorl	%edi, %edi		# any address
	movabsq	$.Lregion, %rsi
	movl	$3, %edx		# PROT_READ|PROT_WRITE
	movl	$0x24022, %r10d		# MAP_PRIVATE|MAP_ANONYMOUS|MAP_STACK|MAP_NORESERVE
	movq	$-1, %r8
	xorl	%r9d, %r9d
	movl	$9, %eax		# mmap
	syscall
	cmpq	$-4095, %rax
	jae	.Lnostack
	movq	%rax, aether.gregion(%rip)
	movq	%rax, %rdi
	movabsq	$.Lregion, %rsi
	movl	$15, %edx		# MADV_NOHUGEPAGE; a failure is harmless
	movl	$28, %eax		# madvise
	syscall
	popq	%rdi
1:	movq	.Lgfree(%rip), %rax
	testq	%rax, %rax
	jz	2f
	movq	16(%rax), %rcx		# a freed slot
	movq	%rcx, .Lgfree(%rip)
	jmp	3f
2:	movq	aether.gused(%rip), %rcx
	cmpq	$.Lnslots, %rcx
	jae	.Lnostack
	incq	%rcx			# a slot never used
	movq	%rcx, aether.gused(%rip)
	shlq	$26, %rcx		# * .Lslot
	addq	aether.gregion(%rip), %rcx
	leaq	-.Lrec(%rcx), %rax
3:	movq	$1, 8(%rax)
	movq	$0, 16(%rax)
	leaq	.Lrec+.Lreserve-.Lslot(%rax), %rcx
	movq	%rcx, 24(%rax)		# L
	movq	%rax, 32(%rax)		# the top
	leaq	.Lgostart(%rip), %rcx
	movq	%rcx, -8(%rax)		# the return address
	movq	%rdi, -16(%rax)		# %rbx: the function
	movq	$0, -24(%rax)		# %rbp: the outermost frame
	movq	$0, -32(%rax)		# %r12
	movq	$0, -40(%rax)		# %r13
	movq	$0, -48(%rax)		# %r14
	movq	$0, -56(%rax)		# %r15
	leaq	-56(%rax), %rcx
	movq	%rcx, (%rax)		# the saved %rsp
	ret
	.size	aether.gonew, .-aether.gonew

# .Lgostart is where a new goroutine starts, with %rsp at its stack's top,
# 16-byte aligned, as _start calls main.main. Its function must never
# return.
	.type	.Lgostart, @function
.Lgostart:
	call	*%rbx
	leaq	.Lreturned(%rip), %rsi
	movl	$.Lreturnedlen, %edx
	jmp	.Lfatal
	.size	.Lgostart, .-.Lgostart

# aether.goswitch suspends the running goroutine and resumes the one whose
# handle is in %rdi. A goroutine that is not running has, at its saved
# %rsp, the six registers in the order they are popped and then the
# address to return to: after its own call of aether.goswitch, or
# .Lgostart.
	.globl	aether.goswitch
	.type	aether.goswitch, @function
aether.goswitch:
	pushq	%rbx
	pushq	%rbp
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	movq	aether.gcur(%rip), %rax
	testq	%rax, %rax
	jnz	1f
	leaq	aether.g0(%rip), %rax
1:	movq	aether.stacklimit(%rip), %rcx
	movq	%rcx, 24(%rax)
	movq	aether.stacktop(%rip), %rcx
	movq	%rcx, 32(%rax)
	movq	%rsp, (%rax)		# after the call below returns, nothing
	call	.Ltarget		# is written below it
	movq	(%rax), %rsp
.Lpop:
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbp
	popq	%rbx
	ret
	.size	aether.goswitch, .-aether.goswitch

# aether.goexit ends the running goroutine, which is not the main one, and
# resumes the one whose handle is in %rdi, another live goroutine. Once on
# that goroutine's stack, it frees the slot it left.
	.globl	aether.goexit
	.type	aether.goexit, @function
aether.goexit:
	movq	aether.gcur(%rip), %r8	# the ending goroutine's record
	testq	%r8, %r8
	jz	.Lbad			# the main goroutine
	cmpq	%rdi, %r8
	je	.Lbad			# itself
	call	.Ltarget
	movq	(%rax), %rsp		# on the resumed goroutine's stack
	movq	$0, 8(%r8)
	leaq	.Lrec-.Lslot(%r8), %rdi	# the slot's base
	movl	$.Lslot-.Lpage, %esi	# all but its top page
	movl	$4, %edx		# MADV_DONTNEED
	movl	$28, %eax		# madvise
	syscall
	movq	.Lgfree(%rip), %rcx
	movq	%rcx, 16(%r8)
	movq	%r8, .Lgfree(%rip)
	jmp	.Lpop
	.size	aether.goexit, .-aether.goexit

# .Ltarget makes the goroutine whose handle is in %rdi the running one: it
# returns its record in %rax, after setting aether.gcur, aether.stacklimit,
# and aether.stacktop from it. The caller then moves to its stack. It
# clobbers %rcx.
	.type	.Ltarget, @function
.Ltarget:
	movq	%rdi, %rax
	testq	%rax, %rax
	jnz	1f
	leaq	aether.g0(%rip), %rax
	jmp	2f
1:	cmpq	$1, 8(%rax)
	jne	.Lbad			# not a live goroutine
2:	movq	%rdi, aether.gcur(%rip)
	movq	24(%rax), %rcx
	movq	%rcx, aether.stacklimit(%rip)
	movq	32(%rax), %rcx
	movq	%rcx, aether.stacktop(%rip)
	ret
	.size	.Ltarget, .-.Ltarget

# .Lbad ends the program after a switch that breaks GoSwitch's or GoExit's
# preconditions, which lowering guarantees: a defect, not Go behavior.
.Lbad:
	leaq	.Lbadmsg(%rip), %rsi
	movl	$.Lbadlen, %edx
	jmp	.Lfatal

.Lnostack:
	leaq	.Lnostackmsg(%rip), %rsi
	movl	$.Lnostacklen, %edx

# .Lfatal writes the message at %rsi, %rdx bytes, and exits with status 2.
.Lfatal:
	andq	$-16, %rsp		# for aether.write
	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached

	.section	.rodata
.Lnostackmsg:
	.ascii	"fatal error: runtime: cannot allocate goroutine stack\n"
	.set	.Lnostacklen, .-.Lnostackmsg
.Lbadmsg:
	.ascii	"fatal error: aether: bad goroutine switch\n"
	.set	.Lbadlen, .-.Lbadmsg
.Lreturned:
	.ascii	"fatal error: aether: goroutine function returned\n"
	.set	.Lreturnedlen, .-.Lreturned

	.bss
	.p2align	3
	.globl	aether.gcur
	.type	aether.gcur, @object
	.size	aether.gcur, 8
aether.gcur:				# the running goroutine's handle
	.zero	8
	.globl	aether.g0
	.type	aether.g0, @object
	.size	aether.g0, .Lrec
aether.g0:				# the main goroutine's record
	.zero	.Lrec
	.globl	aether.gregion
	.type	aether.gregion, @object
	.size	aether.gregion, 8
aether.gregion:				# the region's base, or 0
	.zero	8
	.globl	aether.gused
	.type	aether.gused, @object
	.size	aether.gused, 8
aether.gused:				# the slots ever used, from the lowest
	.zero	8
.Lgfree:				# the free list
	.zero	8

	.section	.note.GNU-stack,"",@progbits
