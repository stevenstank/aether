package runtime

// KeepAlive does nothing: an Aether program's collector sees a pointer as
// live until its last use, and the call is a use of x (D-056).
func KeepAlive(x any) {}
