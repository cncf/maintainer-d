package onboarding

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-github/v55/github"
)

// MockGitHubTransport captures GitHub API calls for testing
type MockGitHubTransport struct {
	mu              sync.Mutex
	requests        []*http.Request
	responses       map[string]*http.Response
	createdComments []GitHubCommentCapture
}

// GitHubCommentCapture represents a captured comment creation
type GitHubCommentCapture struct {
	Owner       string
	Repo        string
	IssueNumber int
	Body        string
}

// NewMockGitHubTransport creates a new mock GitHub HTTP transport
func NewMockGitHubTransport() *MockGitHubTransport {
	return &MockGitHubTransport{
		responses: make(map[string]*http.Response),
	}
}

// RoundTrip implements http.RoundTripper interface
func (m *MockGitHubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, req)

	// Capture comment creation
	if req.Method == "POST" && strings.Contains(req.URL.Path, "/issues/") && strings.HasSuffix(req.URL.Path, "/comments") {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))

		var comment github.IssueComment
		if json.Unmarshal(body, &comment) != nil {
			return nil, err
		}

		// Parse owner, repo, issue number from URL
		// URL format: /repos/{owner}/{repo}/issues/{issue_number}/comments
		parts := strings.Split(req.URL.Path, "/")
		if len(parts) >= 6 {
			issueNum, err := strconv.Atoi(parts[5])
			if err != nil {
				return nil, err
			}
			capture := GitHubCommentCapture{
				Owner:       parts[2],
				Repo:        parts[3],
				IssueNumber: issueNum,
				Body:        comment.GetBody(),
			}
			m.createdComments = append(m.createdComments, capture)
		}

		// Return success response
		resp := &http.Response{
			StatusCode: 201,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"id": 1}`))),
			Header:     make(http.Header),
		}
		return resp, nil
	}

	// Return configured response or default 200
	key := req.Method + " " + req.URL.Path
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
		Header:     make(http.Header),
	}, nil
}

// GetCreatedComments returns all captured comments
func (m *MockGitHubTransport) GetCreatedComments() []GitHubCommentCapture {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]GitHubCommentCapture{}, m.createdComments...)
}

// GetRequests returns all captured HTTP requests
func (m *MockGitHubTransport) GetRequests() []*http.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*http.Request{}, m.requests...)
}

// Reset clears all captured data
func (m *MockGitHubTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = nil
	m.createdComments = nil
}

// SetResponse configures a custom response for a specific request
func (m *MockGitHubTransport) SetResponse(method, path string, statusCode int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := method + " " + path
	m.responses[key] = &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Header:     make(http.Header),
	}
}
