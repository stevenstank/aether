# Aether runtime: process entry (D-009, D-022).
#
# _start runs main.main on a stack the runtime maps itself, so the generated
# frame never depends on the process stack limit (ulimit -s). The program
# declares in aether.mainstack how many bytes main.main uses below the call;
# _start adds .Lreserve for the runtime helpers, rounds up to whole pages,
# and puts a PROT_NONE guard page below. If the stack cannot be allocated,
# it prints a fatal error and exits with status 2. Otherwise it calls
# main.main and exits with status 0 through exit_group.

	.set	.Lpage, 4096
	.set	.Lreserve, 4096		# helpers use at most 56 bytes below %rsp

	.text
	.globl	_start
	.type	_start, @function
_start:
	xorl	%ebp, %ebp		# outermost frame
	andq	$-16, %rsp
	movq	aether.mainstack(%rip), %rsi
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

	.section	.rodata
.Lnostack:
	.ascii	"fatal error: cannot allocate the main stack\n"
	.set	.Lnostacklen, .-.Lnostack

	.section	.note.GNU-stack,"",@progbits
