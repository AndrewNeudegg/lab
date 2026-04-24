package llm

func NewOllama(baseURL string) *OpenAICompatible {
	return NewOpenAICompatible("ollama", baseURL, "")
}
