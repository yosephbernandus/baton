package knowledge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type LSPClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	nextID  int
	mu      sync.Mutex
}

type lspRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type lspResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *lspError       `json:"error,omitempty"`
}

type lspError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	SymbolKindClass     = 5
	SymbolKindMethod    = 6
	SymbolKindField     = 8
	SymbolKindInterface = 11
	SymbolKindFunction  = 12
	SymbolKindVariable  = 13
	SymbolKindConstant  = 14
	SymbolKindStruct    = 23
)

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          LSPRange         `json:"range"`
	SelectionRange LSPRange         `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
	Detail         string           `json:"detail,omitempty"`
}

type LSPRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

type LSPPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type WorkspaceSymbol struct {
	Name          string      `json:"name"`
	Kind          int         `json:"kind"`
	Location      LSPLocation `json:"location"`
	ContainerName string      `json:"containerName,omitempty"`
}

type LSPLocation struct {
	URI   string   `json:"uri"`
	Range LSPRange `json:"range"`
}

func NewLSPClient(command string, args []string, rootDir string) (*LSPClient, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = rootDir
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", command, err)
	}

	client := &LSPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}

	if err := client.initialize(rootDir); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	return client, nil
}

func (c *LSPClient) initialize(rootDir string) error {
	absDir, _ := filepath.Abs(rootDir)
	rootURI := pathToURI(absDir)

	params := map[string]interface{}{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"documentSymbol": map[string]interface{}{
					"hierarchicalDocumentSymbolSupport": true,
					"symbolKind": map[string]interface{}{
						"valueSet": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
					},
				},
			},
			"workspace": map[string]interface{}{
				"symbol": map[string]interface{}{
					"symbolKind": map[string]interface{}{
						"valueSet": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
					},
				},
			},
		},
		"workspaceFolders": []map[string]string{
			{"uri": rootURI, "name": filepath.Base(absDir)},
		},
	}

	_, err := c.request("initialize", params)
	if err != nil {
		return err
	}

	return c.notify("initialized", map[string]interface{}{})
}

func (c *LSPClient) OpenFile(filePath string) error {
	absPath, _ := filepath.Abs(filePath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        pathToURI(absPath),
			"languageId": detectLangID(absPath),
			"version":    1,
			"text":       string(content),
		},
	}

	return c.notify("textDocument/didOpen", params)
}

func (c *LSPClient) CloseFile(filePath string) {
	absPath, _ := filepath.Abs(filePath)
	params := map[string]interface{}{
		"textDocument": map[string]string{
			"uri": pathToURI(absPath),
		},
	}
	c.notify("textDocument/didClose", params)
}

func (c *LSPClient) DocumentSymbols(filePath string) ([]DocumentSymbol, error) {
	absPath, _ := filepath.Abs(filePath)
	params := map[string]interface{}{
		"textDocument": map[string]string{
			"uri": pathToURI(absPath),
		},
	}

	result, err := c.request("textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	var symbols []DocumentSymbol
	if err := json.Unmarshal(result, &symbols); err != nil {
		return nil, fmt.Errorf("parsing symbols: %w", err)
	}
	return symbols, nil
}

func detectLangID(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".py", ".pyi":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".go":
		return "go"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".c":
		return "c"
	case ".h", ".hpp":
		return "cpp"
	default:
		return "plaintext"
	}
}

func (c *LSPClient) WorkspaceSymbols(query string) ([]WorkspaceSymbol, error) {
	params := map[string]interface{}{
		"query": query,
	}

	result, err := c.request("workspace/symbol", params)
	if err != nil {
		return nil, err
	}

	var symbols []WorkspaceSymbol
	if err := json.Unmarshal(result, &symbols); err != nil {
		return nil, fmt.Errorf("parsing workspace symbols: %w", err)
	}
	return symbols, nil
}

func (c *LSPClient) Shutdown() error {
	_, err := c.request("shutdown", nil)
	if err != nil {
		c.cmd.Process.Kill()
		return err
	}
	c.notify("exit", nil)
	return c.cmd.Wait()
}

func (c *LSPClient) request(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	req := lspRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.send(req); err != nil {
		return nil, fmt.Errorf("sending %s: %w", method, err)
	}

	resp, err := c.readResponse()
	if err != nil {
		return nil, fmt.Errorf("reading response for %s: %w", method, err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

func (c *LSPClient) notify(method string, params interface{}) error {
	req := lspRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.send(req)
}

func (c *LSPClient) send(msg interface{}) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(body)
	return err
}

func (c *LSPClient) readResponse() (*lspResponse, error) {
	for {
		contentLen, err := c.readHeader()
		if err != nil {
			return nil, err
		}

		body := make([]byte, contentLen)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return nil, fmt.Errorf("reading body: %w", err)
		}

		// Check if this is a notification or server request (no "id" or has "method")
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("parsing message: %w", err)
		}

		// Server notifications and requests have "method" field
		if _, hasMethod := raw["method"]; hasMethod {
			continue
		}

		var resp lspResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		return &resp, nil
	}
}

func (c *LSPClient) readHeader() (int, error) {
	var contentLen int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("reading header: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLen, err = strconv.Atoi(val)
			if err != nil {
				return 0, fmt.Errorf("parsing content-length %q: %w", val, err)
			}
		}
	}
	if contentLen == 0 {
		return 0, fmt.Errorf("missing Content-Length header")
	}
	return contentLen, nil
}

func pathToURI(path string) string {
	// LSP requires file:///absolute/path (three slashes for Unix paths)
	// url.PathEscape would escape slashes, which we don't want
	// Only escape special chars in path segments
	return "file://" + path
}

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		path := strings.TrimPrefix(uri, "file://")
		if decoded, err := url.PathUnescape(path); err == nil {
			return decoded
		}
		return path
	}
	return uri
}

func SymbolKindName(kind int) string {
	switch kind {
	case SymbolKindClass:
		return "class"
	case SymbolKindMethod:
		return "method"
	case SymbolKindField:
		return "field"
	case SymbolKindInterface:
		return "interface"
	case SymbolKindFunction:
		return "function"
	case SymbolKindVariable:
		return "variable"
	case SymbolKindConstant:
		return "constant"
	case SymbolKindStruct:
		return "struct"
	default:
		return "unknown"
	}
}
