# Aether runtime: cooperative preemption at loop back edges (D-059).
#
# Every loop of the program's own and standard-library code (not
# Aether's overlay files) begins each iteration with ir.Preempt: a test
# of the byte aether.preemptreq, and, if it is set, a call of
# aether.preempt. The request is set by a signal: aether.preemptstart
# (ir.SysPreempt) creates a timer on the process's CPU-time clock that
# sends SIGURG every 10 ms of CPU time, which gc's runtime also uses to
# preempt and ignores otherwise, and whose default action is to ignore
# it. The handler, on an alternate signal stack of its own, only sets
# the byte. A process waiting in a system call uses no CPU time, so the
# timer does not interrupt it; the handler is installed with SA_RESTART
# for the calls the kernel restarts.
#
# aether.preempt clears the request and calls the function SysPreempt
# registered, the scheduler's preempt, which may switch goroutines,
# with every register saved on the goroutine's own stack, where the
# collector scans them (D-046, D-048): at a Preempt, the program's
# values may be in any allocatable register, and in %rax, %rcx, and
# %rdx. No XMM register holds a value across instructions (float.go).

	.set	.Lsigurg, 23
	.set	.Laltsize, 65536

	.text
# aether.preemptstart takes the code address of a function with no
# parameters and no results in %rdi (ir.SysPreempt) and starts the
# timer, under the helper interface. A failure leaves the program
# without preemption, as it was before D-059.
	.globl	aether.preemptstart
	.type	aether.preemptstart, @function
aether.preemptstart:
	pushq	%rbx
	movq	%rdi, aether.preemptfn(%rip)
	cmpb	$0, .Lstarted(%rip)
	jne	9f
	movb	$1, .Lstarted(%rip)
	xorl	%edi, %edi		# any address
	movl	$.Laltsize, %esi
	movl	$3, %edx		# PROT_READ|PROT_WRITE
	movl	$0x22, %r10d		# MAP_PRIVATE|MAP_ANONYMOUS
	movq	$-1, %r8
	xorl	%r9d, %r9d
	movl	$9, %eax		# mmap
	syscall
	cmpq	$-4095, %rax
	jae	9f
	subq	$80, %rsp		# stack_t, struct sigaction, struct sigevent
	movq	%rax, (%rsp)		# ss_sp
	movq	$0, 8(%rsp)		# ss_flags
	movq	$.Laltsize, 16(%rsp)	# ss_size
	movq	%rsp, %rdi
	xorl	%esi, %esi
	movl	$131, %eax		# sigaltstack
	syscall
	testq	%rax, %rax
	jnz	8f
	leaq	.Lhandler(%rip), %rax
	movq	%rax, (%rsp)		# sa_handler
	movq	$0x1c000000, 8(%rsp)	# SA_ONSTACK|SA_RESTART|SA_RESTORER
	leaq	.Lrestorer(%rip), %rax
	movq	%rax, 16(%rsp)		# sa_restorer
	movq	$0, 24(%rsp)		# sa_mask
	movl	$.Lsigurg, %edi
	movq	%rsp, %rsi
	xorl	%edx, %edx
	movl	$8, %r10d		# sizeof(sigset_t)
	movl	$13, %eax		# rt_sigaction
	syscall
	testq	%rax, %rax
	jnz	8f
	movq	$0, (%rsp)		# struct sigevent: sigev_value
	movl	$.Lsigurg, 8(%rsp)	# sigev_signo
	movl	$0, 12(%rsp)		# sigev_notify: SIGEV_SIGNAL
	movq	$0, 16(%rsp)		# the rest of the 64 bytes are unused
	movl	$2, %edi		# CLOCK_PROCESS_CPUTIME_ID
	movq	%rsp, %rsi
	leaq	64(%rsp), %rdx		# the timer's id
	movl	$222, %eax		# timer_create
	syscall
	testq	%rax, %rax
	jnz	8f
	movl	64(%rsp), %edi
	movq	$0, (%rsp)		# it_interval: 10 ms
	movq	$10000000, 8(%rsp)
	movq	$0, 16(%rsp)		# it_value: 10 ms
	movq	$10000000, 24(%rsp)
	xorl	%esi, %esi
	movq	%rsp, %rdx
	xorl	%r10d, %r10d
	movl	$223, %eax		# timer_settime
	syscall
8:	addq	$80, %rsp
9:	popq	%rbx
	ret
	.size	aether.preemptstart, .-aether.preemptstart

# .Lhandler is SIGURG's handler: it requests a preemption. It runs on the
# alternate stack and returns to .Lrestorer.
	.type	.Lhandler, @function
.Lhandler:
	movb	$1, aether.preemptreq(%rip)
	ret
	.size	.Lhandler, .-.Lhandler

	.type	.Lrestorer, @function
.Lrestorer:
	movl	$15, %eax		# rt_sigreturn
	syscall
	hlt				# not reached
	.size	.Lrestorer, .-.Lrestorer

# aether.preempt is called by a Preempt whose request is set, with %rsp
# 16-byte aligned at the call. It preserves every general register but
# %rsp and the flags, and uses 128 bytes below %rsp before the call.
	.globl	aether.preempt
	.type	aether.preempt, @function
aether.preempt:
	pushq	%rax
	pushq	%rbx
	pushq	%rcx
	pushq	%rdx
	pushq	%rsi
	pushq	%rdi
	pushq	%r8
	pushq	%r9
	pushq	%r10
	pushq	%r11
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	pushq	%rbp			# 15 words and the return address: aligned
	movb	$0, aether.preemptreq(%rip)
	call	*aether.preemptfn(%rip)	# ABI-S0 and ABI-R1 alike: no arguments
	popq	%rbp
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%r11
	popq	%r10
	popq	%r9
	popq	%r8
	popq	%rdi
	popq	%rsi
	popq	%rdx
	popq	%rcx
	popq	%rbx
	popq	%rax
	ret
	.size	aether.preempt, .-aether.preempt

	.bss
	.globl	aether.preemptreq
	.type	aether.preemptreq, @object
	.size	aether.preemptreq, 1
# aether.preemptreq is 1 when a preemption is requested, and .Lstarted
# once aether.preemptstart has run.
aether.preemptreq:
	.zero	1
.Lstarted:
	.zero	1
	.p2align	3
	.globl	aether.preemptfn
	.type	aether.preemptfn, @object
	.size	aether.preemptfn, 8
# aether.preemptfn is the function aether.preempt calls.
aether.preemptfn:
	.zero	8

	.section	.note.GNU-stack,"",@progbits
