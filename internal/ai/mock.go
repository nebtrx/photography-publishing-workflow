package ai

import "context"

// Mock is a test-only AI provider that returns pre-configured responses.
type Mock struct {
	Responses []string // responses returned in order; cycles if exhausted
	Calls     []Request // records all calls made
	idx       int
}

func NewMock(responses ...string) *Mock {
	return &Mock{Responses: responses}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Generate(_ context.Context, req Request) (*Response, error) {
	m.Calls = append(m.Calls, req)
	resp := m.Responses[m.idx%len(m.Responses)]
	m.idx++
	return &Response{Text: resp, Model: "mock"}, nil
}
