package llm

type ErrProviderNotImplemented string

func (e ErrProviderNotImplemented) Error() string {
	return string(e)
}
