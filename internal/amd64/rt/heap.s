# Aether runtime: the heap and its garbage collector (D-010, D-035, D-046).
#
# aether.alloc returns in %rax the address of %rdi new bytes, all zero,
# 8-byte aligned. A request for 0 bytes returns aether.zerobase, as gc
# returns its zerobase. If the heap cannot be reserved, or a request does
# not fit in it even after a collection, it writes "fatal error: runtime:
# out of memory" and exits with status 2.
#
# The heap is a 64 GiB MAP_NORESERVE reservation in 8 KiB pages. The page
# table has one word per page: 0 for a free page; for a page of small
# objects, their size class's size (16 to 4096 bytes); for the first page of
# a large object, the bytes of its run of pages; for the other pages of the
# run, minus their distance from the first. Every object starts with an
# 8-byte header: 0 free; otherwise bit 0 (allocated), bit 1 (marked), and
# bit 2 (noscan: aether.allocnoscan's objects, which hold no pointer and
# whose words are not scanned, D-047). The address returned is just after
# it. Free small objects are on their class's free
# list, linked through the word after the header; free pages are all zero.
#
# The collector is conservative, non-moving, mark-sweep (D-046). A
# collection starts when the bytes allocated since the last one reach
# .Lnextgc, or when no run of free pages fits. It marks every object that
# a word of the roots, or of a marked object, points into: the roots are
# the stack, from the collector's %rsp to aether.stacktop, with the
# callee-saved registers pushed on it, .bss, and the stack of every other
# live goroutine (D-048, .Lscangs). Then it sweeps: it rebuilds
# the free lists, frees the pages of small pages with no live object and of
# dead large objects, and releases them to the kernel. With
# AETHER_GCSTRESS=1 in the environment (aether.gcstress, set by _start),
# collections are frequent and freed small objects are filled with
# 0xdeadbeefdeadbeef.
#
# aether.alloc and aether.allocnoscan follow the helper interface: they
# preserve %rbx, %rbp, and %r12-%r15, require %rsp 16-byte aligned at the
# call, and use at most 256 bytes below %rsp.

	.set	.Lheapsize, 68719476736	# 64 GiB
	.set	.Lpage, 8192
	.set	.Lmaxpages, 8388608	# .Lheapsize / .Lpage
	.set	.Lptsize, 67108864	# .Lmaxpages * 8
	.set	.Lmarksize, 34359738368	# one word for each 16 bytes of heap
	.set	.Lnclass, 32
	.set	.Lgcmin, 4194304	# 4 MiB
	.set	.Lstressmin, 8192

	.text
	.globl	aether.allocnoscan
	.type	aether.allocnoscan, @function
aether.allocnoscan:
	pushq	$5			# the header: allocated, noscan
	jmp	.Lalloc
	.size	aether.allocnoscan, .-aether.allocnoscan

	.globl	aether.alloc
	.type	aether.alloc, @function
aether.alloc:
	pushq	$1			# the header: allocated
.Lalloc:
	testq	%rdi, %rdi
	jz	.Lzero
	movabsq	$.Lheapsize, %rax
	cmpq	%rax, %rdi
	ja	.Loom
	cmpq	$0, .Lbase(%rip)
	jne	1f
	call	.Linit
1:	movq	.Lallocated(%rip), %rax
	cmpq	.Lnextgc(%rip), %rax
	jb	2f
	call	.Lgc
2:	leaq	8+15(%rdi), %rax
	andq	$-16, %rax		# the object's bytes, header included
	cmpq	$4096, %rax
	ja	.Llarge
	cmpq	$256, %rax
	ja	3f
	movq	%rax, %rcx
	shrq	$4, %rcx
	decq	%rcx			# the class
	jmp	4f
3:	leaq	63(%rax), %rcx
	shrq	$6, %rcx
	leaq	.Lclass64(%rip), %rdx
	movzbl	(%rdx,%rcx), %ecx
4:	leaq	.Lcsize(%rip), %rdx
	movq	(%rdx,%rcx,8), %rsi	# the class's size
	leaq	.Lfree(%rip), %rdx
	movq	(%rdx,%rcx,8), %r8	# the object
	testq	%r8, %r8
	jnz	5f
	call	.Lrefill
5:	movq	8(%r8), %r9
	movq	%r9, (%rdx,%rcx,8)
	popq	%r9
	movq	%r9, (%r8)
	addq	%rsi, .Lallocated(%rip)
	leaq	8(%r8), %rdi
	leaq	-8(%rsi), %rcx
	shrq	$3, %rcx
	xorl	%eax, %eax
	rep stosq
	leaq	8(%r8), %rax
	ret

# A large object: a run of whole pages, which are zero.
.Llarge:
	leaq	8+.Lpage-1(%rdi), %rdi
	shrq	$13, %rdi		# its pages
	call	.Lpagesgc
	movq	.Lpt(%rip), %r9
	movq	%rdi, %rcx
	shlq	$13, %rcx
	movq	%rcx, (%r9,%rax,8)
	addq	%rcx, .Lallocated(%rip)
	leaq	8(%r9,%rax,8), %r10
	movq	$-1, %rdx
	leaq	-1(%rdi), %r11
1:	testq	%r11, %r11
	jz	2f
	movq	%rdx, (%r10)
	decq	%rdx
	addq	$8, %r10
	decq	%r11
	jmp	1b
2:	shlq	$13, %rax
	addq	.Lbase(%rip), %rax
	popq	%r9
	movq	%r9, (%rax)
	addq	$8, %rax
	ret

.Lzero:
	addq	$8, %rsp		# the header
	leaq	aether.zerobase(%rip), %rax
	ret

.Loom:
	andq	$-16, %rsp		# for aether.write; the program ends here
	leaq	.Loommsg(%rip), %rsi
	movl	$.Loomlen, %edx
	call	aether.write
	movl	$231, %eax		# exit_group
	movl	$2, %edi
	syscall
	hlt				# not reached
	.size	aether.alloc, .-aether.alloc

# .Linit reserves the heap, the page table, and the mark stack. It
# preserves %rdi.
	.type	.Linit, @function
.Linit:
	pushq	%rdi
	movabsq	$.Lheapsize, %rsi
	call	.Lmmap
	movq	%rax, .Lbase(%rip)
	movl	$.Lptsize, %esi
	call	.Lmmap
	movq	%rax, .Lpt(%rip)
	movabsq	$.Lmarksize, %rsi
	call	.Lmmap
	movq	%rax, .Lmark(%rip)
	movq	$.Lgcmin, .Lnextgc(%rip)
	cmpq	$0, aether.gcstress(%rip)
	je	1f
	movq	$.Lstressmin, .Lnextgc(%rip)
1:	popq	%rdi
	ret
	.size	.Linit, .-.Linit

# .Lmmap reserves %rsi bytes, zero, with MAP_NORESERVE, and returns their
# address in %rax, or ends the program.
	.type	.Lmmap, @function
.Lmmap:
	xorl	%edi, %edi		# any address
	movl	$3, %edx		# PROT_READ|PROT_WRITE
	movl	$0x4022, %r10d		# MAP_PRIVATE|MAP_ANONYMOUS|MAP_NORESERVE
	movq	$-1, %r8		# no file
	xorl	%r9d, %r9d
	movl	$9, %eax		# mmap
	syscall				# clobbers %rcx and %r11
	cmpq	$-4095, %rax
	jae	.Loom			# -errno
	ret
	.size	.Lmmap, .-.Lmmap

# .Lrefill puts a new page of objects of class %rcx, of %rsi bytes, on the
# class's free list, %rdx pointing to the free lists, and returns the list's
# head in %r8. It preserves %rcx, %rsi, and %rdx.
	.type	.Lrefill, @function
.Lrefill:
	pushq	%rcx
	pushq	%rsi
	pushq	%rdx
	movl	$1, %edi
	call	.Lpagesgc
	popq	%rdx
	popq	%rsi
	popq	%rcx
	movq	.Lpt(%rip), %r9
	movq	%rsi, (%r9,%rax,8)
	shlq	$13, %rax
	addq	.Lbase(%rip), %rax	# the page, zero
	leaq	.Lpage(%rax), %r10	# its end
	movq	%rax, %r8
	movq	%rax, %r9
1:	leaq	(%r9,%rsi), %r11	# the next object
	leaq	(%r11,%rsi), %rdi
	cmpq	%r10, %rdi
	ja	2f			# it does not fit: %r9 is the last
	movq	%r11, 8(%r9)
	movq	%r11, %r9
	jmp	1b
2:	movq	(%rdx,%rcx,8), %rdi	# a collection may have filled the list
	movq	%rdi, 8(%r9)
	movq	%r8, (%rdx,%rcx,8)
	ret
	.size	.Lrefill, .-.Lrefill

# .Lpagesgc returns in %rax the first of %rdi free pages in a row, after a
# collection if none fit, or ends the program. It preserves %rdi.
	.type	.Lpagesgc, @function
.Lpagesgc:
	call	.Lpages
	testq	%rax, %rax
	jns	1f
	call	.Lgc
	call	.Lpages
	testq	%rax, %rax
	js	.Loom
1:	ret
	.size	.Lpagesgc, .-.Lpagesgc

# .Lpages returns in %rax the first of %rdi free pages in a row, first fit
# from .Lhint, raising .Lhigh if the run reaches it, or -1. It preserves
# %rdi, %rsi, %rdx, and %r8.
	.type	.Lpages, @function
.Lpages:
	movq	.Lpt(%rip), %r9
	movq	.Lhint(%rip), %rcx	# the page looked at
	movq	.Lhigh(%rip), %r10
	movq	%rcx, %rax		# the run's first page
	xorl	%r11d, %r11d		# the run's length
1:	cmpq	%r10, %rcx
	jae	3f
	cmpq	$0, (%r9,%rcx,8)
	jne	2f
	testq	%r11, %r11
	jnz	4f
	movq	%rcx, %rax
4:	incq	%rcx
	incq	%r11
	cmpq	%rdi, %r11
	jb	1b
	jmp	5f
2:	incq	%rcx
	xorl	%r11d, %r11d
	jmp	1b
3:	testq	%r11, %r11		# the run ends at .Lhigh: extend it
	jnz	6f
	movq	%r10, %rax
6:	leaq	(%rax,%rdi), %rcx
	cmpq	$.Lmaxpages, %rcx
	ja	7f
	movq	%rcx, .Lhigh(%rip)
5:	cmpq	.Lhint(%rip), %rax
	jne	8f
	leaq	(%rax,%rdi), %rcx
	movq	%rcx, .Lhint(%rip)
8:	ret
7:	movq	$-1, %rax
	ret
	.size	.Lpages, .-.Lpages

# aether.gc collects, unless nothing was ever allocated (ir.SysGC, for
# runtime.GC: D-049, increment 11.6). It follows the helper interface.
	.globl	aether.gc
	.type	aether.gc, @function
aether.gc:
	cmpq	$0, .Lbase(%rip)
	je	1f
	call	.Lgc
1:	ret
	.size	aether.gc, .-aether.gc

# .Lgc collects. It preserves %rdi and the callee-saved registers. While it
# runs, %r15 is the heap's base, %rbx the page table, %rbp the bytes of the
# pages below .Lhigh, and %r14 the top of the mark stack.
	.type	.Lgc, @function
.Lgc:
	pushq	%rbx
	pushq	%rbp
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	pushq	%rdi
	movq	.Lbase(%rip), %r15
	movq	.Lpt(%rip), %rbx
	movq	.Lhigh(%rip), %rbp
	shlq	$13, %rbp
	movq	.Lmark(%rip), %r14
	movq	%rsp, %r12		# the stack, with the registers pushed above
	movq	aether.stacktop(%rip), %r13
	call	.Lscan
	leaq	__bss_start(%rip), %r12
	addq	$7, %r12
	andq	$-8, %r12
	leaq	_end(%rip), %r13
	call	.Lscan
	call	.Lscangs
1:	cmpq	.Lmark(%rip), %r14	# the marked objects' words
	je	2f
	subq	$8, %r14
	movq	(%r14), %r12
	testq	$4, (%r12)
	jnz	1b			# noscan
	movq	%r12, %rax
	subq	%r15, %rax
	shrq	$13, %rax
	movq	(%rbx,%rax,8), %r13	# the object's bytes
	addq	%r12, %r13
	addq	$8, %r12
	call	.Lscan
	jmp	1b
2:	call	.Lsweep
	popq	%rdi
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbp
	popq	%rbx
	ret
	.size	.Lgc, .-.Lgc

# .Lscangs scans the stack of every live goroutine that is not running
# (D-048, goroutine.s), from its saved %rsp, below which it pushed its
# registers, to its top: the main goroutine's, if it is not running, and
# those in the slots used so far. The running goroutine's stack is the one
# from the collector's %rsp to aether.stacktop.
	.type	.Lscangs, @function
.Lscangs:
	cmpq	$0, aether.gcur(%rip)
	je	1f
	leaq	aether.g0(%rip), %r9
	movq	(%r9), %r12
	movq	32(%r9), %r13
	call	.Lscan
1:	movq	aether.gused(%rip), %r10
	movq	aether.gregion(%rip), %r9
	addq	$0x4000000-64, %r9	# the first slot's record
2:	testq	%r10, %r10
	jz	4f
	cmpq	$1, 8(%r9)
	jne	3f			# free
	cmpq	aether.gcur(%rip), %r9
	je	3f			# running
	movq	(%r9), %r12
	movq	32(%r9), %r13
	call	.Lscan
3:	addq	$0x4000000, %r9		# the next slot's
	decq	%r10
	jmp	2b
4:	ret
	.size	.Lscangs, .-.Lscangs

# .Lscan marks what each word from %r12 to %r13 points into.
	.type	.Lscan, @function
.Lscan:
1:	cmpq	%r13, %r12
	jae	2f
	movq	(%r12), %rax
	call	.Lmarkword
	addq	$8, %r12
	jmp	1b
2:	ret
	.size	.Lscan, .-.Lscan

# .Lmarkword marks the allocated object %rax points into, if any, and
# pushes it on the mark stack. It clobbers %rax, %rcx, %rdx, %rsi, and %r8.
	.type	.Lmarkword, @function
.Lmarkword:
	movq	%rax, %rcx
	subq	%r15, %rcx		# the offset in the heap
	cmpq	%rbp, %rcx
	jae	9f
	movq	%rcx, %rdx
	shrq	$13, %rdx		# the page
	movq	(%rbx,%rdx,8), %rsi
	testq	%rsi, %rsi
	jz	9f			# free
	jns	1f
	addq	%rsi, %rdx		# a large object's first page
	movq	(%rbx,%rdx,8), %rsi
1:	shlq	$13, %rdx
	leaq	(%r15,%rdx), %r8	# the page's address
	cmpq	$.Lpage, %rsi
	jae	2f			# a large object starts it
	subq	%rdx, %rcx		# the offset in the page
	movq	%rcx, %rax
	xorl	%edx, %edx
	divq	%rsi
	imulq	%rsi, %rax		# the object's offset
	leaq	(%rax,%rsi), %rdx
	cmpq	$.Lpage, %rdx
	ja	9f			# the page's tail
	addq	%rax, %r8
2:	movq	(%r8), %rax
	andl	$3, %eax
	cmpl	$1, %eax
	jne	9f			# free, or marked
	orq	$2, (%r8)
	movq	%r8, (%r14)
	addq	$8, %r14
9:	ret
	.size	.Lmarkword, .-.Lmarkword

# .Lsweep frees what is not marked and clears the marks, rebuilds the free
# lists, releases the free pages to the kernel, and sets .Lnextgc. %r12 is
# the page, %r13 .Lhigh, %r14 the live bytes, %rbp the first of the free
# pages before %r12, or -1.
	.type	.Lsweep, @function
.Lsweep:
	leaq	.Lfree(%rip), %rdi
	movl	$.Lnclass, %ecx
	xorl	%eax, %eax
	rep stosq
	movq	.Lhigh(%rip), %r13
	movq	%r13, .Lhint(%rip)
	xorl	%r12d, %r12d
	xorl	%r14d, %r14d
	movq	$-1, %rbp
1:	cmpq	%r13, %r12
	jae	.Lswend
	movq	(%rbx,%r12,8), %rsi
	testq	%rsi, %rsi
	jz	.Lswfree
	movq	%r12, %r8
	shlq	$13, %r8
	addq	%r15, %r8		# the page's address
	cmpq	$.Lpage, %rsi
	jae	.Lswlarge
	leaq	.Lpage(%r8), %r10	# a page of small objects: its end
	movq	%r8, %r9
	xorl	%r11d, %r11d		# its live bytes
2:	leaq	(%r9,%rsi), %rdi
	cmpq	%r10, %rdi
	ja	4f
	movq	(%r9), %rcx
	testq	%rcx, %rcx
	jz	5f			# free
	testq	$2, %rcx
	jz	3f
	andq	$-3, (%r9)		# live: the mark cleared
	addq	%rsi, %r11
	jmp	5f
3:	movq	$0, (%r9)		# dead
	cmpq	$0, aether.gcstress(%rip)
	je	5f
	leaq	8(%r9), %rdi
	leaq	-8(%rsi), %rcx
	shrq	$3, %rcx
	movabsq	$0xdeadbeefdeadbeef, %rax
	rep stosq
5:	addq	%rsi, %r9
	jmp	2b
4:	testq	%r11, %r11
	jz	6f
	addq	%r11, %r14
	pushq	%rsi
	call	.Lflush
	popq	%rsi
	cmpq	$256, %rsi		# the class, for the free list
	ja	7f
	movq	%rsi, %rcx
	shrq	$4, %rcx
	decq	%rcx
	jmp	8f
7:	movq	%rsi, %rcx
	shrq	$6, %rcx
	leaq	.Lclass64(%rip), %rdx
	movzbl	(%rdx,%rcx), %ecx
8:	leaq	.Lfree(%rip), %rdx
	movq	(%rdx,%rcx,8), %rax
	movq	%r8, %r9
9:	leaq	(%r9,%rsi), %rdi
	cmpq	%r10, %rdi
	ja	10f
	cmpq	$0, (%r9)
	jne	11f
	movq	%rax, 8(%r9)
	movq	%r9, %rax
11:	movq	%rdi, %r9
	jmp	9b
10:	movq	%rax, (%rdx,%rcx,8)
	incq	%r12
	jmp	1b
6:	movq	$0, (%rbx,%r12,8)	# no live object: the page is free
.Lswfree:
	testq	%rbp, %rbp
	jns	12f
	movq	%r12, %rbp
12:	incq	%r12
	jmp	1b
.Lswlarge:
	movq	%rsi, %rcx
	shrq	$13, %rcx		# its pages
	testq	$2, (%r8)
	jz	13f
	andq	$-3, (%r8)
	addq	%rsi, %r14
	pushq	%rcx
	call	.Lflush
	popq	%rcx
	addq	%rcx, %r12
	jmp	1b
13:	testq	%rbp, %rbp		# dead: its pages are free
	jns	14f
	movq	%r12, %rbp
14:	movq	$0, (%rbx,%r12,8)
	incq	%r12
	decq	%rcx
	jnz	14b
	jmp	1b
.Lswend:
	call	.Lflush
	movq	%r14, %rax
	cmpq	$0, aether.gcstress(%rip)
	je	15f
	shrq	$3, %rax
	addq	$.Lstressmin, %rax
	jmp	16f
15:	cmpq	$.Lgcmin, %rax
	jae	16f
	movl	$.Lgcmin, %eax
16:	movq	%rax, .Lnextgc(%rip)
	movq	$0, .Lallocated(%rip)
	ret
	.size	.Lsweep, .-.Lsweep

# .Lflush releases the free pages from %rbp to %r12, if %rbp is not -1, to
# the kernel, which will read them as zero, and lowers .Lhint to them. It
# clobbers %rax, %rcx, %rdx, %rsi, %rdi, and %r11.
	.type	.Lflush, @function
.Lflush:
	testq	%rbp, %rbp
	js	2f
	cmpq	.Lhint(%rip), %rbp
	jae	1f
	movq	%rbp, .Lhint(%rip)
1:	movq	%rbp, %rdi
	shlq	$13, %rdi
	addq	%r15, %rdi
	movq	%r12, %rsi
	subq	%rbp, %rsi
	shlq	$13, %rsi
	movl	$4, %edx		# MADV_DONTNEED
	movl	$28, %eax		# madvise
	syscall
	movq	$-1, %rbp
2:	ret
	.size	.Lflush, .-.Lflush

	.section	.rodata
.Loommsg:
	.ascii	"fatal error: runtime: out of memory\n"
	.set	.Loomlen, .-.Loommsg
	.p2align	3
# The size of each class: by 16 bytes to 256, then by quarters of a power
# of two to 4096.
.Lcsize:
	.quad	16, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256
	.quad	320, 384, 448, 512, 640, 768, 896, 1024, 1280, 1536, 1792, 2048, 2560, 3072, 3584, 4096
# The class of the sizes over 256 bytes, by ceil(size / 64).
.Lclass64:
	.byte	16, 16, 16, 16, 16, 16, 17, 18, 19, 20, 20, 21, 21, 22, 22, 23, 23
	.byte	24, 24, 24, 24, 25, 25, 25, 25, 26, 26, 26, 26, 27, 27, 27, 27
	.byte	28, 28, 28, 28, 28, 28, 28, 28, 29, 29, 29, 29, 29, 29, 29, 29
	.byte	30, 30, 30, 30, 30, 30, 30, 30, 31, 31, 31, 31, 31, 31, 31, 31

	.bss
	.p2align	3
.Lbase:
	.zero	8
.Lpt:
	.zero	8
.Lmark:
	.zero	8
.Lhigh:					# the pages below it have been used
	.zero	8
.Lhint:					# no page below it is free
	.zero	8
.Lallocated:				# bytes allocated since the last collection
	.zero	8
.Lnextgc:
	.zero	8
.Lfree:
	.zero	8*.Lnclass
	.globl	aether.zerobase
	.type	aether.zerobase, @object
	.size	aether.zerobase, 8
aether.zerobase:
	.zero	8
	.globl	aether.stacktop
	.type	aether.stacktop, @object
	.size	aether.stacktop, 8
aether.stacktop:
	.zero	8
	.globl	aether.gcstress
	.type	aether.gcstress, @object
	.size	aether.gcstress, 8
aether.gcstress:
	.zero	8

	.section	.note.GNU-stack,"",@progbits
