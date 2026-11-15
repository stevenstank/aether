# Aether runtime: slices (D-036).
#
# aether.memmove copies %rdx bytes from %rsi to %rdi as if through a
# temporary, so the ranges may overlap: forward unless the destination
# starts inside the source, and then backward. It returns nothing, clears the
# direction flag again, and uses 8 bytes below %rsp, its return address.
#
# aether.panicbounds ends the program with a slice bounds error: x in %rdi,
# y in %rsi, and in %edx the format's number (ir.PanicSliceAlen and on,
# counted from 0) times 2, plus 1 if x is signed. It writes "panic: runtime
# error: slice bounds out of range ", then the format's four parts around x
# and y as gc does (ir.BoundsMessage): a negative signed x is followed by the
# fourth part alone. Then a newline, and it exits with status 2. It uses at
# most 72 bytes below %rsp, as aether.panicindex.
#
# aether.panicmakelen, aether.panicmakecap, and aether.panicgrowlen write
# their fixed messages and exit 2; they use 16 bytes below %rsp, as
# aether.panicdivide.
#
# All follow the helper interface: %rbx, %rbp, and %r12-%r15 are preserved,
# and %rsp is 16-byte aligned at the call.

	.text
	.globl	aether.memmove
	.type	aether.memmove, @function
aether.memmove:
	movq	%rdx, %rcx
	testq	%rcx, %rcx
	jz	2f
	movq	%rdi, %rax
	subq	%rsi, %rax		# dst - src, unsigned
	cmpq	%rcx, %rax
	jb	1f			# dst lies in [src, src+n): backward
	rep movsb
	ret
1:	leaq	-1(%rsi,%rcx), %rsi
	leaq	-1(%rdi,%rcx), %rdi
	std
	rep movsb
	cld
2:	ret
	.size	aether.memmove, .-aether.memmove

	.globl	aether.panicbounds
	.type	aether.panicbounds, @function
aether.panicbounds:
	subq	$8, %rsp		# align for the helpers' calls
	movq	%rdi, %rbx		# x
	movq	%rsi, %r12		# y
	movl	%edx, %r13d		# format * 2 + signed
	movl	%edx, %r14d
	shrl	$1, %r14d
	shll	$6, %r14d		# 64 bytes per format
	leaq	.Lboundsfmts(%rip), %r15
	addq	%r14, %r15		# the format's four parts
	leaq	.Lpanicslice(%rip), %rsi
	movl	$.Lpanicslicelen, %edx
	call	aether.write
	movq	0(%r15), %rsi
	movq	8(%r15), %rdx
	call	aether.write
	movq	%rbx, %rdi
	testl	$1, %r13d
	jz	1f
	call	aether.printint
	testq	%rbx, %rbx
	jns	2f
	movq	48(%r15), %rsi		# a negative x: the fourth part alone
	movq	56(%r15), %rdx
	call	aether.write
	jmp	3f
1:	call	aether.printuint
2:	movq	16(%r15), %rsi
	movq	24(%r15), %rdx
	call	aether.write
	movq	%r12, %rdi
	call	aether.printint
	movq	32(%r15), %rsi
	movq	40(%r15), %rdx
	call	aether.write
3:	call	aether.printnl
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.panicbounds, .-aether.panicbounds

	.globl	aether.panicmakelen
	.type	aether.panicmakelen, @function
aether.panicmakelen:
	leaq	.Lmakelen(%rip), %rsi
	movl	$.Lmakelenlen, %edx
	jmp	.Lfixed
	.size	aether.panicmakelen, .-aether.panicmakelen

	.globl	aether.panicmakecap
	.type	aether.panicmakecap, @function
aether.panicmakecap:
	leaq	.Lmakecap(%rip), %rsi
	movl	$.Lmakecaplen, %edx
	jmp	.Lfixed
	.size	aether.panicmakecap, .-aether.panicmakecap

	.globl	aether.panicgrowlen
	.type	aether.panicgrowlen, @function
aether.panicgrowlen:
	leaq	.Lgrowlen(%rip), %rsi
	movl	$.Lgrowlenlen, %edx
.Lfixed:
	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.panicgrowlen, .-aether.panicgrowlen

	.section	.rodata
.Lpanicslice:
	.ascii	"panic: runtime error: slice bounds out of range "
	.set	.Lpanicslicelen, .-.Lpanicslice
.Lmakelen:
	.ascii	"panic: runtime error: makeslice: len out of range\n"
	.set	.Lmakelenlen, .-.Lmakelen
.Lmakecap:
	.ascii	"panic: runtime error: makeslice: cap out of range\n"
	.set	.Lmakecaplen, .-.Lmakecap
.Lgrowlen:
	.ascii	"panic: runtime error: growslice: len out of range\n"
	.set	.Lgrowlenlen, .-.Lgrowlen

# The parts, each a string's address and length.
.Lb_open:	.ascii	"["
.Lb_openc:	.ascii	"[:"
.Lb_opencc:	.ascii	"[::"
.Lb_close:	.ascii	"]"
.Lb_colon:	.ascii	":"
.Lb_colonclose:	.ascii	":]"
.Lb_cc:	.ascii	"::]"
.Lb_len:	.ascii	"] with length "
.Lb_cap:	.ascii	"] with capacity "
	.p2align	3
.Lboundsfmts:
	# PanicSliceAlen: [:x] with length y
	.quad	.Lb_openc, 2, .Lb_len, 14, .Lb_close, 0, .Lb_close, 1
	# PanicSliceAcap: [:x] with capacity y
	.quad	.Lb_openc, 2, .Lb_cap, 16, .Lb_close, 0, .Lb_close, 1
	# PanicSliceB: [x:y]
	.quad	.Lb_open, 1, .Lb_colon, 1, .Lb_close, 1, .Lb_colonclose, 2
	# PanicSlice3Alen: [::x] with length y
	.quad	.Lb_opencc, 3, .Lb_len, 14, .Lb_close, 0, .Lb_close, 1
	# PanicSlice3Acap: [::x] with capacity y
	.quad	.Lb_opencc, 3, .Lb_cap, 16, .Lb_close, 0, .Lb_close, 1
	# PanicSlice3B: [:x:y]
	.quad	.Lb_openc, 2, .Lb_colon, 1, .Lb_close, 1, .Lb_colonclose, 2
	# PanicSlice3C: [x:y:]
	.quad	.Lb_open, 1, .Lb_colon, 1, .Lb_colonclose, 2, .Lb_cc, 3

	.section	.note.GNU-stack,"",@progbits
