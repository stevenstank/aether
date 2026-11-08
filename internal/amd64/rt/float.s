# Aether runtime: printing floating-point values (D-032).
#
# aether.printfloat64 writes the float64 whose IEEE 754 bits are in %rdi, and
# aether.printfloat32 the float32 whose bits are in the low 32 bits of %rdi,
# to file descriptor 2 as gc's print and println do (go1.26.1
# runtime.printfloat64 and printfloat32): strconv's 'g' format with the
# shortest decimal that rounds back to the value, "NaN", "+Inf", "-Inf", "0",
# and "-0". Like the other helpers they require %rsp to be 16-byte aligned at
# the call, return nothing, preserve %rbx, %rbp, and %r12-%r15, and may
# clobber every other general-purpose register. Neither uses a vector
# register. Each uses at most 120 bytes below %rsp: the return address, five
# saved registers, a 64-byte buffer, and one return address for a call.
#
# The digits come from the Dragonbox algorithm as go1.26.1 implements it
# (internal/strconv/ftoadbox.go, dboxFtoa64 and dboxFtoa32); the comments
# give that code's names. aether.pow10 (pow10.s) holds the 128-bit powers of
# ten it reads. Formatting follows formatDigits, fmtE, and fmtF for 'g' with
# the shortest precision.

	.set	.Lpow10min, -292	# the first power in aether.pow10

	.text

	.globl	aether.printfloat64
	.type	aether.printfloat64, @function
aether.printfloat64:
	pushq	%rbx
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	subq	$64, %rsp		# digits end at 32(%rsp); text at 32..63
	movq	%rdi, %r15
	shrq	$63, %r15		# neg
	movq	%rdi, %rbx
	shrq	$52, %rbx
	andl	$0x7ff, %ebx		# biased exponent
	movabsq	$0xfffffffffffff, %r12
	andq	%rdi, %r12		# fraction
	cmpl	$0x7ff, %ebx
	je	.Lspecial
	testl	%ebx, %ebx
	jnz	1f
	testq	%r12, %r12
	jz	.Lzero
	movl	$1, %ebx		# denormal
	jmp	2f
1:	btsq	$52, %r12		# implicit top bit: mant
2:	subq	$1075, %rbx		# e = exp - 1023 - 52
	movabsq	$0x10000000000000, %rax
	cmpq	%rax, %r12
	je	.Lbound64
	# Algorithm 5.2: k0 = -mulLog10_2(e); φ, β = dboxPow64(2+k0, e).
	imulq	$78913, %rbx, %r14
	sarq	$18, %r14
	negq	%r14			# k0
	leaq	2(%r14), %rdi		# k
	leaq	-.Lpow10min(%rdi), %rax
	shlq	$4, %rax
	leaq	aether.pow10(%rip), %rdx
	movq	(%rdx,%rax), %r8	# φ.Hi
	movq	8(%rdx,%rax), %r9	# φ.Lo
	cmpq	$55, %rdi
	jbe	1f			# 0 <= k <= 55
	incq	%r9			# φ.Lo++, without carry
1:	imulq	$108853, %rdi, %rdi
	sarq	$15, %rdi		# mulLog2_10(k)
	addq	%rbx, %rdi		# β = e + e1 - 1
	# zi, exact = dboxMulPow64((2*mant+1) << β, φ)
	leaq	1(%r12,%r12), %rsi
	movl	%edi, %ecx
	shlq	%cl, %rsi		# u
	movq	%rsi, %rax
	mulq	%r9
	movq	%rdx, %r10		# umul128Upper64(u, φ.Lo)
	movq	%rsi, %rax
	mulq	%r8			# umul128(u, φ.Hi)
	addq	%r10, %rax
	adcq	$0, %rdx
	movq	%rax, %r11		# r.Lo: exact iff zero
	movq	%rdx, %rax		# zi
	xorl	%edx, %edx
	movl	$1000, %ecx
	divq	%rcx
	movq	%rax, %r13		# s
	movq	%rdx, %rsi		# r
	movl	$63, %ecx
	subl	%edi, %ecx
	movq	%r8, %r10
	shrq	%cl, %r10		# δi = dboxDelta64(φ, β)
	cmpq	%r10, %rsi
	ja	.Lround64		# r > δi
	je	1f
	testq	%rsi, %rsi		# r < δi
	jnz	.Lshort
	testq	%r11, %r11
	jnz	.Lshort
	testb	$1, %r12b
	jz	.Lshort
	decq	%r13			# s--
	movl	$1000, %esi		# r = 10**(κ+1)
	jmp	.Lround64
1:	leaq	-1(%r12,%r12), %rax	# r == δi
	call	.Lparity64
	testq	%rdx, %rdx
	jnz	.Lshort
	testq	%rax, %rax
	jz	.Lround64
	testb	$1, %r12b
	jz	.Lshort
.Lround64:
	# Algorithm 5.4.
	movq	%r10, %rax
	shrq	$1, %rax
	leaq	50(%rsi), %rbx
	subq	%rax, %rbx		# D = r + 50 - δi/2
	movq	%rbx, %rax
	xorl	%edx, %edx
	movl	$100, %ecx
	divq	%rcx			# t, ρ
	leaq	(%r13,%r13,4), %rcx
	leaq	(%rax,%rcx,2), %r13	# yru = 10*s + t
	testq	%rdx, %rdx
	jnz	.Lnear
	leaq	(%r12,%r12), %rax
	call	.Lparity64
	leaq	-50(%rbx), %rcx
	andl	$1, %ecx
	cmpq	%rcx, %rdx
	jne	1f			# parity != ((D-50)%2 != 0)
	testq	%rax, %rax
	jz	.Lnear
	testb	$1, %r13b
	jz	.Lnear
1:	decq	%r13
	jmp	.Lnear

# .Lparity64 computes dboxParity64(%rax, φ, β) with φ in %r8:%r9 and β in
# %rdi: the parity in %rdx and isInt in %rax, each 0 or 1. It clobbers %rcx
# and %r11.
.Lparity64:
	movq	%rax, %r11
	imulq	%r8, %r11		# mant2 * φ.Hi, low 64 bits
	mulq	%r9			# umul128(mant2, φ.Lo)
	addq	%rdx, %r11		# r.Hi
	movl	$64, %ecx
	subl	%edi, %ecx
	shrq	%cl, %rax		# r.Lo >> (64-β)
	movq	%r11, %rdx
	shrq	%cl, %rdx
	andl	$1, %edx		# parity
	movl	%edi, %ecx
	shlq	%cl, %r11		# r.Hi << β
	orq	%r11, %rax
	sete	%al
	movzbl	%al, %eax		# isInt
	ret

.Lbound64:
	# Algorithm 5.6: k0 = -mulLog10_2MinusLog10_4Over3(e).
	imulq	$631305, %rbx, %r14
	subq	$261663, %r14
	sarq	$21, %r14
	negq	%r14			# k0
	leaq	-.Lpow10min(%r14), %rax
	shlq	$4, %rax
	leaq	aether.pow10(%rip), %rdx
	movq	(%rdx,%rax), %r8	# φ.Hi
	imulq	$108853, %r14, %rdi
	sarq	$15, %rdi
	addq	%rbx, %rdi		# β
	movl	$11, %ecx
	subl	%edi, %ecx		# 64 - 52 - 1 - β
	movq	%r8, %rsi
	shrq	$54, %rsi
	movq	%r8, %r10
	subq	%rsi, %r10
	shrq	%cl, %r10		# xi
	movq	%r8, %rsi
	shrq	$53, %rsi
	addq	%r8, %rsi
	shrq	%cl, %rsi		# zi
	movl	$10, %ecx
	subl	%edi, %ecx		# 64 - 52 - 2 - β
	shrq	%cl, %r8		# for yru
	jmp	.Lbound

	.size	aether.printfloat64, .-aether.printfloat64

	.globl	aether.printfloat32
	.type	aether.printfloat32, @function
aether.printfloat32:
	pushq	%rbx
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	subq	$64, %rsp
	movl	%edi, %r15d
	shrl	$31, %r15d		# neg
	movl	%edi, %ebx
	shrl	$23, %ebx
	andl	$0xff, %ebx		# biased exponent
	movl	%edi, %r12d
	andl	$0x7fffff, %r12d	# fraction
	cmpl	$0xff, %ebx
	je	.Lspecial
	testl	%ebx, %ebx
	jnz	1f
	testl	%r12d, %r12d
	jz	.Lzero
	movl	$1, %ebx
	jmp	2f
1:	btsl	$23, %r12d		# mant
2:	subq	$150, %rbx		# e = exp - 127 - 23
	cmpl	$0x800000, %r12d
	je	.Lbound32
	# k0 = -mulLog10_2(e); φ, β = dboxPow32(1+k0, e).
	imulq	$78913, %rbx, %r14
	sarq	$18, %r14
	negq	%r14			# k0
	leaq	1(%r14), %rdi		# k
	leaq	-.Lpow10min(%rdi), %rax
	shlq	$4, %rax
	leaq	aether.pow10(%rip), %rdx
	movq	(%rdx,%rax), %r8	# φ = m.Hi
	cmpq	$27, %rdi
	jbe	1f			# 0 <= k <= 27
	incq	%r8
1:	imulq	$108853, %rdi, %rdi
	sarq	$15, %rdi
	addq	%rbx, %rdi		# β
	# zi, exact = dboxMulPow32((2*mant+1) << β, φ)
	leaq	1(%r12,%r12), %rsi
	movl	%edi, %ecx
	shlq	%cl, %rsi		# u, below 2**32
	movq	%r8, %rax
	shrq	$32, %rax
	imulq	%rsi, %rax		# u * (φ>>32)
	movl	%r8d, %edx
	imulq	%rsi, %rdx
	shrq	$32, %rdx
	addq	%rdx, %rax		# umul96Upper64(u, φ)
	movl	%eax, %r11d		# exact iff zero
	shrq	$32, %rax		# zi
	xorl	%edx, %edx
	movl	$100, %ecx
	divq	%rcx
	movq	%rax, %r13		# s
	movq	%rdx, %rsi		# r
	movl	$63, %ecx
	subl	%edi, %ecx
	movq	%r8, %r10
	shrq	%cl, %r10		# δi
	cmpq	%r10, %rsi
	ja	.Lround32
	je	1f
	testq	%rsi, %rsi
	jnz	.Lshort
	testl	%r11d, %r11d
	jnz	.Lshort
	testb	$1, %r12b
	jz	.Lshort
	decq	%r13
	movl	$100, %esi
	jmp	.Lround32
1:	leaq	-1(%r12,%r12), %rax
	call	.Lparity32
	testq	%rdx, %rdx
	jnz	.Lshort
	testq	%rax, %rax
	jz	.Lround32
	testb	$1, %r12b
	jz	.Lshort
.Lround32:
	movq	%r10, %rax
	shrq	$1, %rax
	leaq	5(%rsi), %rbx
	subq	%rax, %rbx		# D = r + 5 - δi/2
	movq	%rbx, %rax
	xorl	%edx, %edx
	movl	$10, %ecx
	divq	%rcx			# t, ρ
	leaq	(%r13,%r13,4), %rcx
	leaq	(%rax,%rcx,2), %r13	# yru
	testq	%rdx, %rdx
	jnz	.Lnear
	leaq	(%r12,%r12), %rax
	call	.Lparity32
	leaq	-5(%rbx), %rcx
	andl	$1, %ecx
	cmpq	%rcx, %rdx
	jne	1f
	testq	%rax, %rax
	jz	.Lnear
	testb	$1, %r13b
	jz	.Lnear
1:	decq	%r13
	jmp	.Lnear

# .Lparity32 computes dboxParity32(%rax, φ, β) with φ in %r8 and β in %rdi,
# as .Lparity64 does.
.Lparity32:
	imulq	%r8, %rax		# umul96Lower64(mant2, φ)
	movq	%rax, %rdx
	movl	$64, %ecx
	subl	%edi, %ecx
	shrq	%cl, %rdx
	andl	$1, %edx		# parity
	movl	$32, %ecx
	subl	%edi, %ecx
	shrq	%cl, %rax
	testl	%eax, %eax
	sete	%al
	movzbl	%al, %eax		# isInt
	ret

.Lbound32:
	imulq	$631305, %rbx, %r14
	subq	$261663, %r14
	sarq	$21, %r14
	negq	%r14			# k0
	leaq	-.Lpow10min(%r14), %rax
	shlq	$4, %rax
	leaq	aether.pow10(%rip), %rdx
	movq	(%rdx,%rax), %r8
	cmpq	$27, %r14
	jbe	1f
	incq	%r8			# φ
1:	imulq	$108853, %r14, %rdi
	sarq	$15, %rdi
	addq	%rbx, %rdi		# β
	movl	$40, %ecx
	subl	%edi, %ecx		# 64 - 23 - 1 - β
	movq	%r8, %rsi
	shrq	$25, %rsi
	movq	%r8, %r10
	subq	%rsi, %r10
	shrq	%cl, %r10		# xi
	movq	%r8, %rsi
	shrq	$24, %rsi
	addq	%r8, %rsi
	shrq	%cl, %rsi		# zi
	movl	$39, %ecx
	subl	%edi, %ecx
	shrq	%cl, %r8		# for yru
	jmp	.Lbound

# The rest is shared by both helpers, which have the same frame.

# .Lbound finishes Algorithm 5.6 with e in %rbx, k0 in %r14, xi in %r10, zi
# in %rsi, and φ shifted for yru in %r8.
.Lbound:
	cmpq	$2, %rbx
	je	1f
	cmpq	$3, %rbx
	je	1f
	incq	%r10			# xi++
1:	movq	%rsi, %rax
	xorl	%edx, %edx
	movl	$10, %ecx
	divq	%rcx			# q = zi / 10
	imulq	$10, %rax, %rdx
	cmpq	%rdx, %r10
	jbe	.Lshortq		# xi <= q*10
	leaq	1(%r8), %r13
	shrq	$1, %r13		# yru
	cmpq	$-77, %rbx
	jne	2f
	testb	$1, %r13b
	jz	2f
	decq	%r13
	jmp	.Lnear
2:	cmpq	%r10, %r13
	jae	.Lnear
	incq	%r13
	jmp	.Lnear
.Lshortq:
	movq	%rax, %r13

# .Lshort: the digits are %r13 with trailing zeros removed, and the exponent
# -k0+1 plus the zeros removed (trimZeros, dboxDigits), so %r14 becomes
# k0-1-zeros for .Lnear.
.Lshort:
	decq	%r14
	movl	$10, %ecx
1:	testq	%r13, %r13
	jz	.Lnear
	movq	%r13, %rax
	xorl	%edx, %edx
	divq	%rcx
	testq	%rdx, %rdx
	jnz	.Lnear
	movq	%rax, %r13
	decq	%r14
	jmp	1b

# .Lnear: the digits are those of %r13, and the decimal exponent -%r14
# (dboxDigits). Then format them (formatDigits for 'g', shortest).
.Lnear:
	movq	%r13, %rax
	leaq	32(%rsp), %rsi
	movl	$10, %ecx
1:	xorl	%edx, %edx
	divq	%rcx
	addl	$48, %edx
	decq	%rsi
	movb	%dl, (%rsi)
	testq	%rax, %rax
	jnz	1b
	leaq	32(%rsp), %r8
	subq	%rsi, %r8		# nd
	movq	%r8, %r9
	subq	%r14, %r9		# dp = nd + exp
	leaq	32(%rsp), %rdi		# the text
	testq	%r15, %r15
	jz	1f
	movb	$45, (%rdi)		# '-'
	incq	%rdi
1:	leaq	-1(%r9), %r10		# exp = dp - 1
	cmpq	$-4, %r10
	jl	.Le
	cmpq	$6, %r10
	jge	.Le
	# fmtF with max(nd-dp, 0) fractional digits.
	testq	%r9, %r9
	jg	1f
	movb	$48, (%rdi)		# '0'
	incq	%rdi
	jmp	3f
1:	xorl	%ecx, %ecx		# integer part: dp digits, padded with '0'
2:	movb	$48, %al
	cmpq	%r8, %rcx
	jge	21f
	movb	(%rsi,%rcx), %al
21:	movb	%al, (%rdi)
	incq	%rdi
	incq	%rcx
	cmpq	%r9, %rcx
	jl	2b
3:	movq	%r8, %r11
	subq	%r9, %r11		# fractional digits
	jle	.Lwrite
	movb	$46, (%rdi)		# '.'
	incq	%rdi
	xorl	%ecx, %ecx
4:	leaq	(%r9,%rcx), %rdx	# j = dp + i
	movb	$48, %al
	testq	%rdx, %rdx
	js	41f
	cmpq	%r8, %rdx
	jge	41f
	movb	(%rsi,%rdx), %al
41:	movb	%al, (%rdi)
	incq	%rdi
	incq	%rcx
	cmpq	%r11, %rcx
	jl	4b
	jmp	.Lwrite
.Le:
	# fmtE with nd-1 digits after the point.
	movb	(%rsi), %al
	movb	%al, (%rdi)
	incq	%rdi
	cmpq	$1, %r8
	jle	2f
	movb	$46, (%rdi)
	incq	%rdi
	movl	$1, %ecx
1:	movb	(%rsi,%rcx), %al
	movb	%al, (%rdi)
	incq	%rdi
	incq	%rcx
	cmpq	%r8, %rcx
	jl	1b
2:	movb	$101, (%rdi)		# 'e'
	movb	$43, %al		# '+'
	testq	%r10, %r10
	jns	3f
	movb	$45, %al		# '-'
	negq	%r10
3:	movb	%al, 1(%rdi)
	addq	$2, %rdi
	movq	%r10, %rax
	movl	$10, %ecx
	cmpq	$100, %rax
	jb	4f
	xorl	%edx, %edx		# three digits
	movl	$100, %r11d
	divq	%r11
	addb	$48, %al
	movb	%al, (%rdi)
	incq	%rdi
	movq	%rdx, %rax
	jmp	5f
4:	cmpq	$10, %rax
	jae	5f
	movb	$48, (%rdi)		# one digit, after a '0'
	incq	%rdi
5:	xorl	%edx, %edx		# the last two, or the digit
	divq	%rcx
	cmpq	$10, %r10
	jb	6f
	addb	$48, %al
	movb	%al, (%rdi)
	incq	%rdi
6:	addb	$48, %dl
	movb	%dl, (%rdi)
	incq	%rdi
.Lwrite:
	leaq	32(%rsp), %rsi
	movq	%rdi, %rdx
	subq	%rsi, %rdx
	call	aether.write
.Lreturn:
	addq	$64, %rsp
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbx
	ret

# Infinity or NaN; the fraction is in %r12 and the sign in %r15.
.Lspecial:
	leaq	.Lnan(%rip), %rsi
	movl	$3, %edx
	testq	%r12, %r12
	jnz	1f
	leaq	.Linf(%rip), %rsi
	movl	$4, %edx
	testq	%r15, %r15
	jz	1f
	leaq	.Lneginf(%rip), %rsi
1:	call	aether.write
	jmp	.Lreturn

.Lzero:
	leaq	.Lnegzero+1(%rip), %rsi
	movl	$1, %edx
	testq	%r15, %r15
	jz	1f
	decq	%rsi
	incl	%edx
1:	call	aether.write
	jmp	.Lreturn

	.size	aether.printfloat32, .-aether.printfloat32

	.section	.rodata
.Lnan:
	.ascii	"NaN"
.Linf:
	.ascii	"+Inf"
.Lneginf:
	.ascii	"-Inf"
.Lnegzero:
	.ascii	"-0"

	.section	.note.GNU-stack,"",@progbits
