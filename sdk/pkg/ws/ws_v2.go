package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-admin-team/go-admin-core/sdk/pkg/jwtauth/user"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	// 连接超时设置
	authTimeout        = 60 * time.Second
	heartbeatInterval  = 1 * time.Minute
	connectionLifetime = 60 * time.Minute
	maxMessageSize     = 1024 * 10 // 10KB

	// 资源限制
	MaxTopicsPerClient = 20   // 每个客户端最大订阅主题数
	MaxConnections     = 2000 // 最大连接数
	MaxTopicNameLength = 100  // 主题名最大长度

	// 消息类型
	MsgTypeSubscribe   = "subscribe"
	MsgTypeUnsubscribe = "unsubscribe"
	MsgTypeHeartbeat   = "heartbeat"
	MsgTypeRequest     = "request"  // 客户端请求
	MsgTypeResponse    = "response" // 服务端响应
	MsgTypeData        = "data"
	MsgTypeError       = "error"
)

// 自定义消息协议
type WSMessage struct {
	Type    string          `json:"type"`    // 消息类型
	Topic   string          `json:"topic"`   // 主题/频道
	Payload json.RawMessage `json:"payload"` // 消息内容
}

// ManagerV2 所有 websocket 信息
type ManagerV2 struct {
	Topics               map[string]map[string]*ClientV2 // 主题到客户端的映射
	clientCount          uint64
	Lock                 sync.RWMutex
	Register, UnRegister chan *ClientV2
	Message              chan *MessageDataV2
	BroadcastMessage     chan *BroadcastMessageData
	GroupMessage         chan *TopicMessageData
}

// ClientV2 单个 websocket 信息
type ClientV2 struct {
	ID            string
	UserID        int
	Context       context.Context
	CancelFunc    context.CancelFunc
	Socket        *websocket.Conn
	Message       chan []byte
	Authenticated bool            // 是否已认证
	Subscriptions map[string]bool // 订阅的主题集合
	LastActive    time.Time       // 最后活动时间
	mu            sync.Mutex      // 用于保护Subscriptions
	closeOnce     sync.Once       // 用于确保 Channel 只被关闭一次
}

// MessageDataV2 发送给单个客户的数据
type MessageDataV2 struct {
	Client  *ClientV2
	Message []byte
}

// BroadcastMessageData 广播数据信息
type BroadcastMessageData struct {
	Message []byte
}

// TopicMessageData 群组数据信息
type TopicMessageData struct {
	Topic   string
	Message []byte
}

// 初始化 wsManager 管理器
var WebsocketManagerV2 = &ManagerV2{
	Topics:           make(map[string]map[string]*ClientV2),
	Register:         make(chan *ClientV2, 128),
	UnRegister:       make(chan *ClientV2, 128),
	Message:          make(chan *MessageDataV2, 128),
	BroadcastMessage: make(chan *BroadcastMessageData, 128),
	GroupMessage:     make(chan *TopicMessageData, 128),
}

// 启动管理器
func (m *ManagerV2) Start() {
	log.Println("WebSocket manager started")
	go m.monitorConnections()

	for {
		select {
		case client := <-m.Register:
			m.registerClient(client)

		case client := <-m.UnRegister:
			m.unregisterClient(client)

		case msg := <-m.Message:
			m.sendToClient(msg.Client, msg.Message)

		case broadcast := <-m.BroadcastMessage:
			m.broadcastToAll(broadcast.Message)

		case group := <-m.GroupMessage:
			m.broadcastToTopic(group.Topic, group.Message)
		}
	}
}

// 注册客户端
func (m *ManagerV2) registerClient(c *ClientV2) {
	m.Lock.Lock()
	defer m.Lock.Unlock()

	// 初始化订阅集合
	c.Subscriptions = make(map[string]bool)
	c.LastActive = time.Now()
	atomic.AddUint64(&m.clientCount, 1)

	log.Printf("Client registered - ID: %s", c.ID)
}

// 注销客户端
func (m *ManagerV2) unregisterClient(c *ClientV2) {
	log.Println("[CancelFunc]unregisterClient", c.ID)
	c.closeOnce.Do(func() {
		m.Lock.Lock()
		defer m.Lock.Unlock()

		// 从所有主题中移除
		for topic := range c.Subscriptions {
			if clients, exists := m.Topics[topic]; exists {
				delete(clients, c.ID)
				if len(clients) == 0 {
					delete(m.Topics, topic)
				}
			}
		}
		// 关闭消息通道
		close(c.Message)
		// 取消上下文
		c.CancelFunc()
		// 减少客户端计数
		atomic.AddUint64(&m.clientCount, ^uint64(0))

		log.Printf("Client unregistered - ID: %s", c.ID)
	})
}

// 监控连接状态
func (m *ManagerV2) monitorConnections() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.Lock.RLock()
		now := time.Now()

		// 检查所有客户端状态
		for _, topicClients := range m.Topics {
			for _, client := range topicClients {
				// 检查认证超时
				if !client.Authenticated && now.Sub(client.LastActive) > authTimeout {
					log.Printf("[CancelFunc]Client %s authentication timeout", client.ID)
					client.CancelFunc()
					continue
				}

				// 检查心跳超时
				if now.Sub(client.LastActive) > 2*heartbeatInterval {
					log.Printf("[CancelFunc]Client %s heartbeat timeout", client.ID)
					client.CancelFunc()
					continue
				}

				// 检查连接生命周期
				if now.Sub(client.LastActive) > connectionLifetime {
					log.Printf("[CancelFunc]Client %s connection lifetime expired", client.ID)
					client.CancelFunc()
				}
			}
		}
		m.Lock.RUnlock()
	}
}

// 发送消息到客户端
func (m *ManagerV2) sendToClient(c *ClientV2, msg []byte) {
	select {
	case c.Message <- msg:
	default:
		log.Printf("[CancelFunc]Client %s send buffer full, disconnecting", c.ID)
		c.CancelFunc()
	}
}

// 广播消息到所有客户端
func (m *ManagerV2) broadcastToAll(msg []byte) {
	m.Lock.RLock()
	defer m.Lock.RUnlock()

	clients := m.getAllUniqueClients()
	for _, client := range clients {
		if client.Authenticated {
			m.sendToClient(client, msg)
		}
	}
}

// 广播消息到主题
func (m *ManagerV2) broadcastToTopic(topic string, msg []byte) {
	m.Lock.RLock()
	defer m.Lock.RUnlock()

	if clients, exists := m.Topics[topic]; exists {
		for _, client := range clients {
			if client.Authenticated {
				m.sendToClient(client, msg)
			}
		}
	}
}

// 处理WebSocket连接
func (m *ManagerV2) WsClient(c *gin.Context) {

	m.Lock.RLock()
	if m.clientCount >= MaxConnections {
		m.Lock.RUnlock()
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Service temporarily unavailable - too many connections",
			"code":  "CONNECTION_LIMIT_REACHED",
		})
		return
	}
	m.Lock.RUnlock()

	userID := user.GetUserId(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	timestamp := time.Now().UnixNano()
	clientID := fmt.Sprintf("%d_%d", userID, timestamp)

	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		CheckOrigin:      func(r *http.Request) bool { return true },
		Subprotocols:     []string{c.GetHeader("Sec-WebSocket-Protocol")},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &ClientV2{
		ID:            clientID,
		UserID:        userID,
		Context:       ctx,
		CancelFunc:    cancel,
		Socket:        conn,
		Authenticated: true,
		Message:       make(chan []byte, 256),
	}

	m.Register <- client

	go client.readPump(m)
	go client.writePump()

	log.Printf("create ws client success, clientID: %s", clientID)
}

// 读取消息
func (c *ClientV2) readPump(m *ManagerV2) {
	defer func() {
		WebsocketManagerV2.UnRegister <- c
	}()

	c.Socket.SetReadLimit(maxMessageSize)
	c.Socket.SetReadDeadline(time.Now().Add(2 * heartbeatInterval))

	for {
		_, message, err := c.Socket.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Read error: %v", err)
			}
			break
		}

		c.LastActive = time.Now()
		c.handleMessage(m, message)

		// 读取消息后，重置读取超时时间
		c.Socket.SetReadDeadline(time.Now().Add(2 * heartbeatInterval))
	}
}

// 处理消息
func (c *ClientV2) handleMessage(m *ManagerV2, data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendError("invalid_message_format", "Invalid JSON format")
		return
	}

	switch msg.Type {
	case MsgTypeSubscribe:
		c.handleSubscribe(m, msg.Topic)
	case MsgTypeUnsubscribe:
		c.handleUnsubscribe(m, msg.Topic)
	case MsgTypeHeartbeat:
		c.handleHeartbeat()
	case MsgTypeData:
		// 处理业务数据
		log.Printf("Received data from client %s: %s", c.ID, string(msg.Payload))
	default:
		c.sendError("invalid_message_type", "Unsupported message type")
	}
}

// 处理订阅
func (c *ClientV2) handleSubscribe(m *ManagerV2, topic string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	isValid := isValidTopicName(topic)
	if !isValid {
		c.sendError("invalid_topic", "Invalid topic name")
		return
	}

	if len(c.Subscriptions) >= MaxTopicsPerClient {
		c.sendError("subscription_limit", fmt.Sprintf("Maximum subscription limit reached (%d)", MaxTopicsPerClient))
		return
	}

	if _, exists := c.Subscriptions[topic]; exists {
		c.sendSuccess("subscribe_success", "Already subscribed")
		return
	}

	// 添加到主题
	m.Lock.Lock()
	if _, exists := m.Topics[topic]; !exists {
		m.Topics[topic] = make(map[string]*ClientV2)
	}
	m.Topics[topic][c.ID] = c
	m.Lock.Unlock()

	c.Subscriptions[topic] = true
	c.sendSuccess("subscribe_success", fmt.Sprintf("Subscribed to %s", topic))
	log.Printf("Client %s subscribed to %s", c.ID, topic)
}

// 处理取消订阅
func (c *ClientV2) handleUnsubscribe(m *ManagerV2, topic string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.Subscriptions[topic]; !exists {
		c.sendSuccess("unsubscribe_success", "Not subscribed")
		return
	}

	// 从主题中移除
	m.Lock.Lock()
	if clients, exists := m.Topics[topic]; exists {
		delete(clients, c.ID)
		if len(clients) == 0 {
			delete(m.Topics, topic)
		}
	}
	m.Lock.Unlock()

	delete(c.Subscriptions, topic)
	c.sendSuccess("unsubscribe_success", fmt.Sprintf("Unsubscribed from %s", topic))
	log.Printf("Client %s unsubscribed from %s", c.ID, topic)
}

// 处理心跳
func (c *ClientV2) handleHeartbeat() {
	c.LastActive = time.Now()
}

// 写入消息
func (c *ClientV2) writePump() {
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer func() {
		heartbeatTicker.Stop()
		WebsocketManagerV2.UnRegister <- c
	}()

	for {
		select {
		case message, ok := <-c.Message:
			if !ok {
				c.Socket.WriteMessage(websocket.CloseMessage, nil)
				return
			}

			c.Socket.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Socket.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("Write error: %v", err)
				return
			}

		case <-heartbeatTicker.C:
			c.Socket.SetWriteDeadline(time.Now().Add(10 * time.Second))
			hbMsg, _ := json.Marshal(WSMessage{Type: MsgTypeHeartbeat})
			if err := c.Socket.WriteMessage(websocket.TextMessage, hbMsg); err != nil {
				log.Printf("Heartbeat write error: %v", err)
				return
			}

		case <-c.Context.Done():
			return
		}
	}
}

// 发送消息
func (c *ClientV2) sendMessage(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	select {
	case c.Message <- data:
	default:
		log.Printf("Client %s send buffer full", c.ID)
	}
}

// 发送错误消息
func (c *ClientV2) sendError(code, message string) {
	c.sendMessage(WSMessage{
		Type: MsgTypeError,
		Payload: json.RawMessage(fmt.Sprintf(
			`{"code": "%s", "message": "%s"}`, code, message)),
	})
}

// 发送成功消息
func (c *ClientV2) sendSuccess(code, message string) {
	c.sendMessage(WSMessage{
		Type: "success",
		Payload: json.RawMessage(fmt.Sprintf(
			`{"code": "%s", "message": "%s"}`, code, message)),
	})
}

// 关闭客户端连接
func (m *ManagerV2) CloseConnection(c *gin.Context) {
	clientID := c.Param("id")

	m.Lock.RLock()
	defer m.Lock.RUnlock()

	// 在所有主题中查找客户端
	for _, clients := range m.Topics {
		if client, exists := clients[clientID]; exists {
			log.Println("[CancelFunc]CloseConnection", clientID)
			client.CancelFunc()
			c.JSON(http.StatusOK, gin.H{"status": "disconnected"})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "client not found"})
}

// 发送消息到客户端
func (m *ManagerV2) SendToOne(clientID string, message []byte) {
	msg := WSMessage{
		Type:    MsgTypeData,
		Payload: message,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling client message: %v", err)
		return
	}

	client := m.findClientByID(clientID)
	if client == nil {
		log.Printf("Client %s not found", clientID)
		return
	}

	m.Message <- &MessageDataV2{
		Client:  client,
		Message: data,
	}
}

// 发送消息到主题
func (m *ManagerV2) SendToTopic(topic string, message []byte) {
	msg := WSMessage{
		Type:    MsgTypeData,
		Topic:   topic,
		Payload: message,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling topic message: %v", err)
		return
	}

	m.GroupMessage <- &TopicMessageData{
		Topic:   topic,
		Message: data,
	}
}

// 发送消息到所有客户端
func (m *ManagerV2) SendToAll(message []byte) {
	msg := WSMessage{
		Type:    MsgTypeData,
		Payload: message,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling broadcast message: %v", err)
		return
	}

	m.BroadcastMessage <- &BroadcastMessageData{
		Message: data,
	}
}

// 按需获取所有唯一客户端
func (m *ManagerV2) getAllUniqueClients() map[string]*ClientV2 {
	m.Lock.RLock()
	defer m.Lock.RUnlock()

	uniqueClients := make(map[string]*ClientV2)

	for _, clients := range m.Topics {
		for clientID, client := range clients {
			uniqueClients[clientID] = client
		}
	}

	return uniqueClients
}

// 获取所有客户端ID
func (m *ManagerV2) GetAllClientIDs() []string {
	clients := m.getAllUniqueClients()

	clientIDs := make([]string, 0, len(clients))
	for clientID := range clients {
		clientIDs = append(clientIDs, clientID)
	}

	return clientIDs
}

// 查找客户端
func (m *ManagerV2) findClientByID(clientID string) *ClientV2 {
	m.Lock.RLock()
	defer m.Lock.RUnlock()

	for _, clients := range m.Topics {
		if client, exists := clients[clientID]; exists {
			return client
		}
	}
	return nil
}

// 验证主题名格式
func isValidTopicName(topic string) bool {
	// 检查长度
	if len(topic) > MaxTopicNameLength {
		return false
	}

	// 检查是否为空
	if topic == "" {
		return false
	}

	// 只允许字母、数字、下划线、连字符和点号（用于层级结构）
	validTopicRegex := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	return validTopicRegex.MatchString(topic)
}

// 获取管理器状态
func (m *ManagerV2) Status() map[string]interface{} {
	m.Lock.RLock()
	defer m.Lock.RUnlock()

	topicStats := make(map[string]int)
	for topic, clients := range m.Topics {
		topicStats[topic] = len(clients)
	}

	return map[string]interface{}{
		"topics":        len(m.Topics),
		"clients":       m.clientCount,
		"topic_stats":   topicStats,
		"pending_reg":   len(m.Register),
		"pending_unreg": len(m.UnRegister),
		"pending_msg":   len(m.Message),
		"pending_broad": len(m.BroadcastMessage),
	}
}

func SendTopicV2(topic string, message []byte) {
	WebsocketManagerV2.SendToTopic(topic, message)
}

func SendAllV2(message []byte) {
	WebsocketManagerV2.SendToAll(message)
}

func SendOneV2(clientID string, message []byte) {
	WebsocketManagerV2.SendToOne(clientID, message)
}
