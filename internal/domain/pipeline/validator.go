package pipeline

type CallbackValidator interface {
	Validate(action string, payload []byte) error
}

type NoopValidator struct{}

func (NoopValidator) Validate(string, []byte) error { return nil }
