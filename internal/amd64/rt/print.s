# Aether runtime: println support (D-009, D-021).
#
# aether.printint writes the signed decimal form of %rdi, aether.printsp a
# space, and aether.printnl a newline, to file descriptor 2. Each requires
# %rsp to be 16-byte aligned at the call, returns nothing, preserves %rbx,
# %rbp, and %r12-%r15, and may clobber every other general-purpose register.
#
# Short writes are continued and EINTR is retried. Any other write error is
# ignored and the program continues, as with gc (E7).

	.text

	.globl	aether.printint
	.type	aether.printint, @function
aether.printint:
	subq	$40, %rsp		# 32-byte digit buffer at 0(%rsp)
	movq	%rdi, %r8		# keep the value for its sign
	movq	%rdi, %rax
	testq	%rax, %rax
	js	1f
	negq	%rax			# work with x <= 0: -MinInt64 does not exist
1:	movl	$10, %ecx
	leaq	32(%rsp), %rsi		# one past the last digit
2:	cqto
	idivq	%rcx			# %rax = x / 10 (toward zero); %rdx in [-9, 0]
	movl	$48, %r9d
	subl	%edx, %r9d		# '0' - remainder
	decq	%rsi
	movb	%r9b, (%rsi)
	testq	%rax, %rax
	jnz	2b
	testq	%r8, %r8
	jns	3f
	decq	%rsi
	movb	$45, (%rsi)		# '-'
3:	leaq	32(%rsp), %rdx
	subq	%rsi, %rdx		# length
	call	.Lwrite
	addq	$40, %rsp
	ret
	.size	aether.printint, .-aether.printint

	.globl	aether.printsp
	.type	aether.printsp, @function
aether.printsp:
	leaq	.Lspace(%rip), %rsi
	movl	$1, %edx
	jmp	.Lwrite
	.size	aether.printsp, .-aether.printsp

	.globl	aether.printnl
	.type	aether.printnl, @function
aether.printnl:
	leaq	.Lnewline(%rip), %rsi
	movl	$1, %edx
	jmp	.Lwrite
	.size	aether.printnl, .-aether.printnl

# .Lwrite writes %rdx bytes at %rsi to file descriptor 2. It clobbers %rax,
# %rcx, %rdx, %rsi, %rdi, and %r11.
.Lwrite:
	testq	%rdx, %rdx
	jle	2f
	movl	$1, %eax		# write
	movl	$2, %edi		# standard error
	syscall				# clobbers %rcx and %r11
	cmpq	$-4, %rax		# -EINTR: retry
	je	.Lwrite
	testq	%rax, %rax
	jle	2f			# other error, or no progress: give up
	addq	%rax, %rsi
	subq	%rax, %rdx
	jmp	.Lwrite
2:	ret

	.section	.rodata
.Lspace:
	.byte	32
.Lnewline:
	.byte	10

	.section	.note.GNU-stack,"",@progbits
