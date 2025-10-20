package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"xiaozhi-server-go/src/configs"
	"xiaozhi-server-go/src/core/types"
	"xiaozhi-server-go/src/core/utils"

	go_openai "github.com/sashabaranov/go-openai"
)

// Conn is an interface related to connections, used for sending messages
type Conn interface {
	WriteMessage(messageType int, data []byte) error
}

// Manager MCP service manager
type Manager struct {
	logger                *utils.Logger
	conn                  Conn
	funcHandler           types.FunctionRegistryInterface
	configPath            string
	clients               map[string]MCPClient
	localClient           *LocalClient // Local MCP client
	tools                 []string
	XiaoZhiMCPClient      *XiaoZhiMCPClient // XiaoZhiMCPClient used for handling Xiaozhi MCP related logic
	bRegisteredXiaoZhiMCP bool              // Whether Xiaozhi MCP tools have been registered
	isInitialized         bool              // Add initialization status flag
	systemCfg             *configs.Config
	mu                    sync.RWMutex
}

// NewManagerForPool creates MCP manager for resource pool
func NewManagerForPool(lg *utils.Logger, cfg *configs.Config) *Manager {
	lg.Info("Creating MCP Manager for resource pool")
	projectDir := utils.GetProjectDir()
	configPath := filepath.Join(projectDir, ".mcp_server_settings.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = ""
	}

	mgr := &Manager{
		logger:                lg,
		funcHandler:           nil, // Will be set when binding connection
		conn:                  nil, // Will be set when binding connection
		configPath:            configPath,
		clients:               make(map[string]MCPClient),
		tools:                 make([]string, 0),
		bRegisteredXiaoZhiMCP: false,
		systemCfg:             cfg,
	}
	// Pre-initialize MCP servers that don't depend on connections
	if err := mgr.preInitializeServers(); err != nil {
		lg.Error("Failed to pre-initialize MCP servers: %v", err)
	}

	return mgr
}

// preInitializeServers pre-initializes MCP servers that don't depend on connections
func (m *Manager) preInitializeServers() error {
	m.localClient, _ = NewLocalClient(m.logger, m.systemCfg)
	m.localClient.Start(context.Background())
	m.clients["local"] = m.localClient

	config := m.LoadConfig()
	if config == nil {
		return fmt.Errorf("no valid MCP server configuration found")
	}

	for name, srvConfig := range config {
		// Only initialize external MCP servers that don't require connections
		srvConfigMap, ok := srvConfig.(map[string]interface{})

		if !ok {
			m.logger.Warn("Invalid configuration format for server %s", name)
			continue
		}

		// Create and start external MCP client
		clientConfig, err := convertConfig(srvConfigMap)
		if err != nil {
			m.logger.Error("Failed to convert config for server %s: %v", name, err)
			continue
		}

		client, err := NewClient(clientConfig, m.logger)
		if err != nil {
			m.logger.Error("Failed to create MCP client for server %s: %v", name, err)
			continue
		}

		if err := client.Start(context.Background()); err != nil {
			m.logger.Error("Failed to start MCP client %s: %v", name, err)
			continue
		}
		m.clients[name] = client
	}

	m.isInitialized = true
	return nil
}

// BindConnection binds connection to MCP Manager
func (m *Manager) BindConnection(
	conn Conn,
	fh types.FunctionRegistryInterface,
	params interface{},
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.conn = conn
	m.funcHandler = fh
	paramsMap := params.(map[string]interface{})
	sessionID := paramsMap["session_id"].(string)
	visionURL := paramsMap["vision_url"].(string)
	deviceID := paramsMap["device_id"].(string)
	clientID := paramsMap["client_id"].(string)
	token := paramsMap["token"].(string)
	m.logger.Debug("Binding connection to MCP Manager, sessionID: %s, visionURL: %s", sessionID, visionURL)

	// Optimization: Check if XiaoZhiMCPClient needs to be restarted
	if m.XiaoZhiMCPClient == nil {
		m.XiaoZhiMCPClient = NewXiaoZhiMCPClient(m.logger, conn, sessionID)
		m.clients["xiaozhi"] = m.XiaoZhiMCPClient
		m.XiaoZhiMCPClient.SetVisionURL(visionURL)
		m.XiaoZhiMCPClient.SetID(deviceID, clientID)
		m.XiaoZhiMCPClient.SetToken(token)

		if err := m.XiaoZhiMCPClient.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start XiaoZhi MCP client: %v", err)
		}
	} else {
		// Re-bind connection instead of recreating
		m.XiaoZhiMCPClient.SetConnection(conn)
		m.XiaoZhiMCPClient.SetID(deviceID, clientID)
		m.XiaoZhiMCPClient.SetToken(token)
		if !m.XiaoZhiMCPClient.IsReady() {
			if err := m.XiaoZhiMCPClient.Start(context.Background()); err != nil {
				return fmt.Errorf("failed to restart XiaoZhi MCP client: %v", err)
			}
		}
	}

	// Re-register tools (only register those not yet registered)
	m.registerAllToolsIfNeeded()
	return nil
}

// New method: only register tools when needed
func (m *Manager) registerAllToolsIfNeeded() {
	if m.funcHandler == nil {
		return
	}

	// Check if already registered to avoid duplicate registration
	if !m.bRegisteredXiaoZhiMCP && m.XiaoZhiMCPClient != nil && m.XiaoZhiMCPClient.IsReady() {
		tools := m.XiaoZhiMCPClient.GetAvailableTools()
		for _, tool := range tools {
			toolName := tool.Function.Name
			m.funcHandler.RegisterFunction(toolName, tool)
		}
		m.bRegisteredXiaoZhiMCP = true
	}

	// Register other external MCP client tools
	for name, client := range m.clients {
		if name != "xiaozhi" && client.IsReady() {
			tools := client.GetAvailableTools()
			for _, tool := range tools {
				toolName := tool.Function.Name
				if !m.isToolRegistered(toolName) {
					m.funcHandler.RegisterFunction(toolName, tool)
					m.tools = append(m.tools, toolName)
					// m.logger.Info("Registered external MCP tool: [%s] %s", toolName, tool.Function.Description)
				}
			}
		}
	}
}

// New helper method
func (m *Manager) isToolRegistered(toolName string) bool {
	for _, tool := range m.tools {
		if tool == toolName {
			return true
		}
	}
	return false
}

// Improved Reset method
func (m *Manager) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset connection-related state but keep reusable client structures
	m.conn = nil
	m.funcHandler = nil
	m.bRegisteredXiaoZhiMCP = false
	m.tools = make([]string, 0)

	// Reset connection for xiaozhi client instead of complete destruction
	if m.XiaoZhiMCPClient != nil {
		m.XiaoZhiMCPClient.ResetConnection() // 新增方法
	}

	// Reset connection for external MCP clients
	for name, client := range m.clients {
		if name != "xiaozhi" {
			if resetter, ok := client.(interface{ ResetConnection() error }); ok {
				resetter.ResetConnection()
			}
		}
	}

	return nil
}

// Cleanup implements the Cleanup method of the Provider interface
func (m *Manager) Cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m.CleanupAll(ctx)
	return m.Reset()
}

// LoadConfig loads MCP service configuration
func (m *Manager) LoadConfig() map[string]interface{} {
	if m.configPath == "" {
		return nil
	}

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		m.logger.Error(fmt.Sprintf("Error loading MCP config from %s: %v", m.configPath, err))
		return nil
	}

	var config struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		m.logger.Error(fmt.Sprintf("Error parsing MCP config: %v", err))
		return nil
	}

	return config.MCPServers
}

func (m *Manager) HandleXiaoZhiMCPMessage(msgMap map[string]interface{}) error {
	// Handle Xiaozhi MCP messages
	if m.XiaoZhiMCPClient == nil {
		return fmt.Errorf("XiaoZhiMCPClient is not initialized")
	}
	m.XiaoZhiMCPClient.HandleMCPMessage(msgMap)
	if m.XiaoZhiMCPClient.IsReady() && !m.bRegisteredXiaoZhiMCP {
		// Register Xiaozhi MCP tools
		m.registerTools(m.XiaoZhiMCPClient.GetAvailableTools())
		m.bRegisteredXiaoZhiMCP = true
	}
	return nil
}

// convertConfig converts map configuration to Config structure
func convertConfig(cfg map[string]interface{}) (*Config, error) {
	// Implement conversion from map to Config structure
	config := &Config{
		Enabled: true, // Default enabled
	}

	// Server address
	if addr, ok := cfg["server_address"].(string); ok {
		config.ServerAddress = addr
	}

	// Server port
	if port, ok := cfg["server_port"].(float64); ok {
		config.ServerPort = int(port)
	}

	// Namespace
	if ns, ok := cfg["namespace"].(string); ok {
		config.Namespace = ns
	}

	// Node ID
	if nodeID, ok := cfg["node_id"].(string); ok {
		config.NodeID = nodeID
	}

	// Command line connection method
	if cmd, ok := cfg["command"].(string); ok {
		config.Command = cmd
	}

	// Command line arguments
	if args, ok := cfg["args"].([]interface{}); ok {
		for _, arg := range args {
			if argStr, ok := arg.(string); ok {
				config.Args = append(config.Args, argStr)
			}
		}
	}

	// 环境变量
	if env, ok := cfg["env"].(map[string]interface{}); ok {
		config.Env = make([]string, 0)
		for k, v := range env {
			if vStr, ok := v.(string); ok {
				config.Env = append(config.Env, fmt.Sprintf("%s=%s", k, vStr))
			}
		}
	}

	// SSE连接URL
	if url, ok := cfg["url"].(string); ok {
		config.URL = url
	}

	return config, nil
}

// registerTools 注册工具
func (m *Manager) registerTools(tools []go_openai.Tool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tool := range tools {
		toolName := tool.Function.Name

		// 检查工具是否已注册
		if m.isToolRegistered(toolName) {
			continue // 跳过已注册的工具
		}

		m.tools = append(m.tools, toolName)
		if m.funcHandler != nil {
			if err := m.funcHandler.RegisterFunction(toolName, tool); err != nil {
				m.logger.Error(fmt.Sprintf("Failed to register tool: %s, error: %v", toolName, err))
				continue
			}
			// m.logger.Info("Registered tool: [%s] %s", toolName, tool.Function.Description)
		}
	}
}

// IsMCPTool 检查是否是MCP工具
func (m *Manager) IsMCPTool(toolName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, tool := range m.tools {
		if tool == toolName {
			return true
		}
	}

	return false
}

// ExecuteTool 执行工具调用
func (m *Manager) ExecuteTool(
	ctx context.Context,
	toolName string,
	arguments map[string]interface{},
) (interface{}, error) {
	m.logger.Info(fmt.Sprintf("Executing tool %s with arguments: %v", toolName, arguments))

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, client := range m.clients {
		if client.HasTool(toolName) {
			return client.CallTool(ctx, toolName, arguments)
		}
	}

	return nil, fmt.Errorf("Tool %s not found in any MCP server", toolName)
}

// CleanupAll 依次关闭所有MCPClient
func (m *Manager) CleanupAll(ctx context.Context) {
	m.mu.Lock()
	clients := make(map[string]MCPClient, len(m.clients))
	for name, client := range m.clients {
		clients[name] = client
	}
	m.mu.Unlock()

	for name, client := range clients {
		func() {
			// 设置一个超时上下文
			ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()

			done := make(chan struct{})
			go func() {
				client.Stop()
				close(done)
			}()

			select {
			case <-done:
				m.logger.Info(fmt.Sprintf("MCP client closed: %s", name))
			case <-ctx.Done():
				m.logger.Error(fmt.Sprintf("Timeout closing MCP client %s", name))
			}
		}()

		m.mu.Lock()
		delete(m.clients, name)
		m.mu.Unlock()
	}
}
