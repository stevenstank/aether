# Aether runtime: println support (D-009, D-021).
#
# aether.printint writes the signed decimal form of %rdi, aether.printuint
# its unsigned decimal form, aether.printbool
# "true" if %rdi is 1 and "false" if it is 0, aether.printsp a space, and
# aether.printnl a newline, to file descriptor 2. Each requires
# %rsp to be 16-byte aligned at the call, returns nothing, preserves %rbx,
# %rbp, and %r12-%r15, and may clobber every other general-purpose register.
#
# Short writes are continued and EINTR is retried. Any other write error is
# ignored and the program continues, as with gc (E7).
#
# aether.write, which they share, writes %rdx bytes at %rsi to file
# descriptor 2 under the same rules. It is for the runtime's own use (_start's
# fatal error) and clobbers %rax, %rcx, %rdx, %rsi, %rdi, and %r11.

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
	call	aether.write
	addq	$40, %rsp
	ret
	.size	aether.printint, .-aether.printint

	.globl	aether.printuint
	.type	aether.printuint, @function
aether.printuint:
	subq	$40, %rsp		# 32-byte digit buffer at 0(%rsp)
	movq	%rdi, %rax
	movl	$10, %ecx
	leaq	32(%rsp), %rsi		# one past the last digit
1:	xorl	%edx, %edx
	divq	%rcx			# unsigned: %rax = x / 10, %rdx = x % 10
	addl	$48, %edx		# '0' + remainder
	decq	%rsi
	movb	%dl, (%rsi)
	testq	%rax, %rax
	jnz	1b
	leaq	32(%rsp), %rdx
	subq	%rsi, %rdx		# length
	call	aether.write
	addq	$40, %rsp
	ret
	.size	aether.printuint, .-aether.printuint

	.globl	aether.printbool
	.type	aether.printbool, @function
aether.printbool:
	leaq	.Ltrue(%rip), %rsi
	movl	$4, %edx
	testq	%rdi, %rdi
	jnz	aether.write
	leaq	.Lfalse(%rip), %rsi
	movl	$5, %edx
	jmp	aether.write
	.size	aether.printbool, .-aether.printbool

	.globl	aether.printsp
	.type	aether.printsp, @function
aether.printsp:
	leaq	.Lspace(%rip), %rsi
	movl	$1, %edx
	jmp	aether.write
	.size	aether.printsp, .-aether.printsp

	.globl	aether.printnl
	.type	aether.printnl, @function
aether.printnl:
	leaq	.Lnewline(%rip), %rsi
	movl	$1, %edx
	jmp	aether.write
	.size	aether.printnl, .-aether.printnl

	.globl	aether.write
	.type	aether.write, @function
aether.write:
	testq	%rdx, %rdx
	jle	2f
	movl	$1, %eax		# write
	movl	$2, %edi		# standard error
	syscall				# clobbers %rcx and %r11
	cmpq	$-4, %rax		# -EINTR: retry
	je	aether.write
	testq	%rax, %rax
	jle	2f			# other error, or no progress: give up
	addq	%rax, %rsi
	subq	%rax, %rdx
	jmp	aether.write
2:	ret
	.size	aether.write, .-aether.write

	.section	.rodata
.Lspace:
	.byte	32
.Lnewline:
	.byte	10
.Ltrue:
	.ascii	"true"
.Lfalse:
	.ascii	"false"

	.section	.note.GNU-stack,"",@progbits
