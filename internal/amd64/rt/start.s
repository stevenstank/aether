# Aether runtime: process entry (D-009).
#
# _start aligns the stack, calls the compiled main.main, and exits the
# process with status 0 through exit_group.

	.text
	.globl	_start
	.type	_start, @function
_start:
	xorl	%ebp, %ebp		# outermost frame
	andq	$-16, %rsp		# %rsp is 16-byte aligned at the call
	call	main.main
	movl	$231, %eax		# exit_group
	xorl	%edi, %edi		# status 0
	syscall
	hlt				# not reached
	.size	_start, .-_start

	.section	.note.GNU-stack,"",@progbits
