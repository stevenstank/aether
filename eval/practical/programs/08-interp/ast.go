package main

// Expressions.
type Expr interface{ line() int }

type (
	IntLit struct {
		Line int
		Val  int64
	}
	StrLit struct {
		Line int
		Val  string
	}
	BoolLit struct {
		Line int
		Val  bool
	}
	NilLit struct{ Line int }
	Name   struct {
		Line int
		Name string
	}
	Unary struct {
		Line int
		Op   string
		X    Expr
	}
	Binary struct {
		Line int
		Op   string
		X, Y Expr
	}
	Call struct {
		Line int
		Fn   Expr
		Args []Expr
	}
	Index struct {
		Line int
		X, I Expr
	}
	Field struct {
		Line int
		X    Expr
		Name string
	}
	ListLit struct {
		Line  int
		Elems []Expr
	}
	MapLit struct {
		Line int
		Keys []Expr
		Vals []Expr
	}
	FuncLit struct {
		Line   int
		Name   string
		Params []string
		Body   []Stmt
	}
)

func (e *IntLit) line() int  { return e.Line }
func (e *StrLit) line() int  { return e.Line }
func (e *BoolLit) line() int { return e.Line }
func (e *NilLit) line() int  { return e.Line }
func (e *Name) line() int    { return e.Line }
func (e *Unary) line() int   { return e.Line }
func (e *Binary) line() int  { return e.Line }
func (e *Call) line() int    { return e.Line }
func (e *Index) line() int   { return e.Line }
func (e *Field) line() int   { return e.Line }
func (e *ListLit) line() int { return e.Line }
func (e *MapLit) line() int  { return e.Line }
func (e *FuncLit) line() int { return e.Line }

// Statements.
type Stmt interface{ stmt() }

type (
	LetStmt struct {
		Line int
		Name string
		Init Expr
	}
	AssignStmt struct {
		Line   int
		Target Expr // *Name or *Index
		Op     string
		Val    Expr
	}
	ExprStmt  struct{ X Expr }
	PrintStmt struct {
		Line int
		Args []Expr
	}
	IfStmt struct {
		Line int
		Cond Expr
		Then []Stmt
		Else []Stmt
	}
	WhileStmt struct {
		Line int
		Cond Expr
		Body []Stmt
	}
	ForStmt struct {
		Line int
		Var  string
		Seq  Expr
		Body []Stmt
	}
	ReturnStmt struct {
		Line int
		Val  Expr
	}
	BreakStmt    struct{ Line int }
	ContinueStmt struct{ Line int }
)

func (*LetStmt) stmt()      {}
func (*AssignStmt) stmt()   {}
func (*ExprStmt) stmt()     {}
func (*PrintStmt) stmt()    {}
func (*IfStmt) stmt()       {}
func (*WhileStmt) stmt()    {}
func (*ForStmt) stmt()      {}
func (*ReturnStmt) stmt()   {}
func (*BreakStmt) stmt()    {}
func (*ContinueStmt) stmt() {}
