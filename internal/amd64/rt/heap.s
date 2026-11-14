# Aether runtime: the heap (D-010, D-035).
#
# aether.alloc returns in %rax the address of %rdi new bytes, all zero,
# 8-byte aligned. It is a bump allocator that never frees: on the first call
# it reserves .Lheapsize bytes with MAP_NORESERVE, whose pages the kernel
# zeroes, and then hands out consecutive blocks of them, each rounded up to
# a multiple of 8 bytes. A request for 0 bytes returns aether.zerobase, as
# gc returns its zerobase. If the reservation fails or is used up, it writes
# "fatal error: runtime: out of memory" and exits with status 2.
#
# It follows the helper interface: it preserves %rbx, %rbp, and %r12-%r15,
# requires %rsp 16-byte aligned at the call, and uses at most 24 bytes
# below %rsp: its return address, the saved size, and aether.write's return
# address on the failure path.

	.set	.Lheapsize, 68719476736	# 64 GiB

	.text
	.globl	aether.alloc
	.type	aether.alloc, @function
aether.alloc:
	testq	%rdi, %rdi
	jz	3f
	movq	.Lheapnext(%rip), %rax
	testq	%rax, %rax
	jz	2f			# the first allocation
1:	addq	$7, %rdi
	jc	4f			# a size near 2^64: out of memory
	andq	$-8, %rdi
	movq	.Lheaplimit(%rip), %rcx
	subq	%rax, %rcx		# the bytes left
	cmpq	%rdi, %rcx
	jb	4f
	leaq	(%rax,%rdi), %rcx
	movq	%rcx, .Lheapnext(%rip)
	ret
2:	pushq	%rdi			# the size; %rsp is now 16-byte aligned
	xorl	%edi, %edi		# any address
	movabsq	$.Lheapsize, %rsi
	movl	$3, %edx		# PROT_READ|PROT_WRITE
	movl	$0x4022, %r10d		# MAP_PRIVATE|MAP_ANONYMOUS|MAP_NORESERVE
	movq	$-1, %r8		# no file
	xorl	%r9d, %r9d
	movl	$9, %eax		# mmap
	syscall				# clobbers %rcx and %r11
	popq	%rdi
	cmpq	$-4095, %rax
	jae	4f			# -errno
	movq	%rax, .Lheapnext(%rip)
	movabsq	$.Lheapsize, %rcx
	addq	%rax, %rcx
	movq	%rcx, .Lheaplimit(%rip)
	jmp	1b
3:	leaq	aether.zerobase(%rip), %rax
	ret
4:	andq	$-16, %rsp		# for aether.write; the program ends here
	leaq	.Loom(%rip), %rsi
	movl	$.Loomlen, %edx
	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.alloc, .-aether.alloc

	.section	.rodata
.Loom:
	.ascii	"fatal error: runtime: out of memory\n"
	.set	.Loomlen, .-.Loom

	.bss
	.p2align	3
.Lheapnext:
	.zero	8
.Lheaplimit:
	.zero	8
	.globl	aether.zerobase
	.type	aether.zerobase, @object
	.size	aether.zerobase, 8
aether.zerobase:
	.zero	8

	.section	.note.GNU-stack,"",@progbits
