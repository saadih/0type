//go:build !windows

package hotkey

// New returns a no-op controller off Windows (the global hook is Windows-only).
func New(b Binding) Controller { return noopController{} }

type noopController struct{}

func (noopController) Start(onPress, onRelease func()) error { return nil }
func (noopController) SetBinding(b Binding)                  {}
func (noopController) Capture() (Binding, error)             { return Binding{}, nil }
