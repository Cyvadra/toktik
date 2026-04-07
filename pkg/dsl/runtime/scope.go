package runtime

// Scope holds variable bindings with lexical parent chaining.
type Scope struct {
	parent *Scope
	vars   map[string]Value
}

// NewScope creates a top-level scope.
func NewScope() *Scope {
	return &Scope{vars: make(map[string]Value)}
}

// Child creates a child scope for a block or function body.
func (s *Scope) Child() *Scope {
	return &Scope{parent: s, vars: make(map[string]Value)}
}

// Get looks up a variable, walking the parent chain.
func (s *Scope) Get(name string) (Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	if s.parent != nil {
		return s.parent.Get(name)
	}
	return NaVal(), false
}

// Set defines or updates a variable in the current scope.
func (s *Scope) Set(name string, v Value) {
	s.vars[name] = v
}

// Update updates a variable in the scope where it was defined.
func (s *Scope) Update(name string, v Value) bool {
	if _, ok := s.vars[name]; ok {
		s.vars[name] = v
		return true
	}
	if s.parent != nil {
		return s.parent.Update(name, v)
	}
	return false
}

// Fn represents a user-defined function or a native builtin.
type Fn struct {
	Name   string
	Params []string
	// For user-defined functions, Body is non-nil and Native is nil.
	Body interface{} // *ast.Block — kept as interface to avoid circular import
	// For builtins, Native is set.
	Native func(args []Value) Value
	// Closure captures the defining scope (user-defined functions only).
	Closure *Scope
}
