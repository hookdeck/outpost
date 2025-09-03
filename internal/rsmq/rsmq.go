package rsmq

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

// Unset values are the special values to refer default values of the attributes
const (
	UnsetVt      = ^uint(0)
	UnsetDelay   = ^uint(0)
	UnsetMaxsize = -(int(^uint(0)>>1) - 1)
)

// SendMessageOption is a function that configures sendMessageOptions
type SendMessageOption func(*sendMessageOptions)

type sendMessageOptions struct {
	id string
}

// WithMessageID returns a SendMessageOption that sets a custom message ID
func WithMessageID(id string) SendMessageOption {
	return func(o *sendMessageOptions) {
		o.id = id
	}
}

const (
	q      = ":Q"
	queues = "QUEUES"
)

const (
	defaultNs      = "rsmq"
	defaultVt      = 30
	defaultDelay   = 0
	defaultMaxsize = 65536
)

// Errors returned on rsmq operation
var (
	ErrQueueNotFound   = errors.New("queue not found")
	ErrQueueExists     = errors.New("queue exists")
	ErrMessageTooLong  = errors.New("message too long")
	ErrMessageNotFound = errors.New("message not found")
)

var (
	hashPopMessage              = redis.NewScript(scriptPopMessage).Hash()
	hashReceiveMessage          = redis.NewScript(scriptReceiveMessage).Hash()
	hashChangeMessageVisibility = redis.NewScript(scriptChangeMessageVisibility).Hash()
)

// RedisClient interface defines the operations needed by RSMQ
// Both redis.Client and redis.ClusterClient implement these methods
type RedisClient interface {
	Time() *redis.TimeCmd
	HSetNX(key, field string, value interface{}) *redis.BoolCmd
	HMGet(key string, fields ...string) *redis.SliceCmd
	SMembers(key string) *redis.StringSliceCmd
	SAdd(key string, members ...interface{}) *redis.IntCmd
	ZCard(key string) *redis.IntCmd
	ZCount(key, min, max string) *redis.IntCmd
	ZAdd(key string, members ...redis.Z) *redis.IntCmd
	HSet(key, field string, value interface{}) *redis.BoolCmd
	HIncrBy(key, field string, incr int64) *redis.IntCmd
	Del(keys ...string) *redis.IntCmd
	HDel(key string, fields ...string) *redis.IntCmd
	ZRem(key string, members ...interface{}) *redis.IntCmd
	SRem(key string, members ...interface{}) *redis.IntCmd
	EvalSha(sha1 string, keys []string, args ...interface{}) *redis.Cmd
	ScriptLoad(script string) *redis.StringCmd
	TxPipeline() redis.Pipeliner
	Exists(keys ...string) *redis.IntCmd
	Type(key string) *redis.StatusCmd
	Close() error
}

// RedisSMQ is the client of rsmq to execute queue and message operations
type RedisSMQ struct {
	client RedisClient
	ns     string
	logger *logging.Logger
}

// QueueAttributes contains some attributes and stats of queue
type QueueAttributes struct {
	Vt         uint
	Delay      uint
	Maxsize    int
	TotalRecv  uint64
	TotalSent  uint64
	Created    uint64
	Modified   uint64
	Msgs       uint64
	HiddenMsgs uint64
}

type queueDef struct {
	vt      uint
	delay   uint
	maxsize int
	ts      uint64
	uid     string
	qname   string
}

// QueueMessage contains content and metadata of message received from queue
type QueueMessage struct {
	ID      string
	Message string
	Rc      uint64
	Fr      time.Time
	Sent    time.Time
}

// NewRedisSMQ creates and returns new rsmq client
func NewRedisSMQ(client RedisClient, ns string, logger ...*logging.Logger) *RedisSMQ {
	if client == nil {
		panic("")
	}

	if ns == "" {
		ns = defaultNs
	}
	if !strings.HasSuffix(ns, ":") {
		ns += ":"
	}

	var l *logging.Logger
	if len(logger) > 0 {
		l = logger[0]
	}

	rsmq := &RedisSMQ{
		client: client,
		ns:     ns,
		logger: l,
	}

	client.ScriptLoad(scriptPopMessage)
	client.ScriptLoad(scriptReceiveMessage)
	client.ScriptLoad(scriptChangeMessageVisibility)

	return rsmq
}

// CreateQueue creates a new queue with given attributes
// to create new queue with default attributes:
//
//	err:=redisRsmq.CreateQueue(qname,rsmq.UnsetVt,rsmq.UnsetDelay,rsmq.UnsetMaxsize)
func (rsmq *RedisSMQ) CreateQueue(qname string, vt uint, delay uint, maxsize int) error {
	if vt == UnsetVt {
		vt = defaultVt
	}
	if delay == UnsetDelay {
		delay = defaultDelay
	}
	if maxsize == UnsetMaxsize {
		maxsize = defaultMaxsize
	}

	if err := validateQname(qname); err != nil {
		return err
	}
	if err := validateVt(vt); err != nil {
		return err
	}
	if err := validateDelay(delay); err != nil {
		return err
	}
	if err := validateMaxsize(maxsize); err != nil {
		return err
	}

	t, err := rsmq.client.Time().Result()
	if err != nil {
		if rsmq.logger != nil {
			rsmq.logger.Debug("Redis TIME command failed during queue creation", 
				zap.Error(err), 
				zap.String("queue", qname))
		}
		return err
	}

	key := rsmq.ns + qname + q

	tx := rsmq.client.TxPipeline()
	r := tx.HSetNX(key, "vt", vt)
	tx.HSetNX(key, "delay", delay)
	tx.HSetNX(key, "maxsize", maxsize)
	tx.HSetNX(key, "created", t.Unix())
	tx.HSetNX(key, "modified", t.Unix())
	
	if rsmq.logger != nil {
		rsmq.logger.Debug("executing Redis transaction for queue creation",
			zap.String("queue", qname),
			zap.String("key", key),
			zap.Uint("vt", vt),
			zap.Uint("delay", delay),
			zap.Int("maxsize", maxsize))
			
		// Check if key already exists and what type it is
		keyType, typeErr := rsmq.client.Type(key).Result()
		if typeErr == nil {
			rsmq.logger.Debug("Redis key diagnostics before transaction",
				zap.String("key", key),
				zap.String("key_type", keyType))
		}
	}
	
	_, err = tx.Exec()
	if err != nil {
		if rsmq.logger != nil {
			rsmq.logger.Error("Redis transaction execution failed during queue creation", 
				zap.Error(err),
				zap.String("queue", qname),
				zap.String("key", key),
				zap.String("context", "This likely indicates Redis connectivity or permission issues"))
				
			// Try to get more details about what might be wrong
			keyExists, existsErr := rsmq.client.Exists(key).Result()
			keyType, typeErr := rsmq.client.Type(key).Result()
			if existsErr == nil && typeErr == nil {
				rsmq.logger.Error("Redis key state after failed transaction",
					zap.String("key", key),
					zap.Int64("exists", keyExists),
					zap.String("type", keyType))
			}
		}
		return err
	}
	
	if !r.Val() {
		if rsmq.logger != nil {
			rsmq.logger.Debug("queue creation skipped - queue already exists", 
				zap.String("queue", qname))
		}
		return ErrQueueExists
	}

	_, err = rsmq.client.SAdd(rsmq.ns+queues, qname).Result()
	if err != nil {
		if rsmq.logger != nil {
			rsmq.logger.Error("failed to add queue to queue set", 
				zap.Error(err),
				zap.String("queue", qname),
				zap.String("set_key", rsmq.ns+queues))
		}
	} else {
		if rsmq.logger != nil {
			rsmq.logger.Debug("queue created successfully", 
				zap.String("queue", qname))
		}
	}
	return err
}

func (rsmq *RedisSMQ) getQueue(qname string, uid bool) (*queueDef, error) {
	key := rsmq.ns + qname + q

	tx := rsmq.client.TxPipeline()

	hmGetSliceCmd := tx.HMGet(key, "vt", "delay", "maxsize")
	timeCmd := tx.Time()
	if _, err := tx.Exec(); err != nil {
		return nil, err
	}

	hmGetValues := hmGetSliceCmd.Val()
	if hmGetValues[0] == nil || hmGetValues[1] == nil || hmGetValues[2] == nil {
		return nil, ErrQueueNotFound
	}

	vt := convertStringToUint[uint](hmGetValues[0])
	delay := convertStringToUint[uint](hmGetValues[1])
	maxsize := convertStringToInt[int](hmGetValues[2])

	t := timeCmd.Val()

	randUID := ""
	if uid {
		randUID = strconv.FormatInt(t.UnixNano()/1000, 36) + makeID(22)
	}

	return &queueDef{
		vt:      vt,
		delay:   delay,
		maxsize: maxsize,
		ts:      uint64(t.UnixMilli()),
		uid:     randUID,
		qname:   qname,
	}, nil
}

// ListQueues returns the slice consist of the existing queues
func (rsmq *RedisSMQ) ListQueues() ([]string, error) {
	return rsmq.client.SMembers(rsmq.ns + queues).Result()
}

// GetQueueAttributes returns queue attributes
func (rsmq *RedisSMQ) GetQueueAttributes(qname string) (*QueueAttributes, error) {
	if err := validateQname(qname); err != nil {
		return nil, err
	}

	queue, err := rsmq.getQueue(qname, false)
	if err != nil {
		return nil, err
	}

	key := rsmq.ns + qname

	tx := rsmq.client.TxPipeline()
	hmGetSliceCmd := tx.HMGet(key+q, "vt", "delay", "maxsize", "totalrecv", "totalsent", "created", "modified")
	zCardIntCmd := tx.ZCard(key)
	zCountIntCmd := tx.ZCount(key, strconv.FormatInt(int64(queue.ts), 10), "+inf")
	if _, err := tx.Exec(); err != nil {
		return nil, err
	}

	hmGetValues := hmGetSliceCmd.Val()

	vt := convertStringToUint[uint](hmGetValues[0])
	delay := convertStringToUint[uint](hmGetValues[1])
	maxsize := convertStringToInt[int](hmGetValues[2])
	totalRecv := convertStringToUnsignedOrDefault[uint64](hmGetValues[3], 0)
	totalSent := convertStringToUnsignedOrDefault[uint64](hmGetValues[4], 0)
	created := convertStringToUint[uint64](hmGetValues[5])
	modified := convertStringToUint[uint64](hmGetValues[6])

	msgs := uint64(zCardIntCmd.Val())
	hiddenMsgs := uint64(zCountIntCmd.Val())

	return &QueueAttributes{
		Vt:         vt,
		Delay:      delay,
		Maxsize:    maxsize,
		TotalRecv:  totalRecv,
		TotalSent:  totalSent,
		Created:    created,
		Modified:   modified,
		Msgs:       msgs,
		HiddenMsgs: hiddenMsgs,
	}, nil

}

// SetQueueAttributes sets queue attributes
// to not change some attributes:
//
//	queAttrib,err:=redisRsmq.CreateQueue(qname,rsmq.UnsetVt,rsmq.UnsetDelay,newMaxsize)
func (rsmq *RedisSMQ) SetQueueAttributes(qname string, vt uint, delay uint, maxsize int) (*QueueAttributes, error) {
	if err := validateQname(qname); err != nil {
		return nil, err
	}

	queue, err := rsmq.getQueue(qname, false)
	if err != nil {
		return nil, err
	}

	if vt == UnsetVt {
		vt = queue.vt
	}
	if delay == UnsetDelay {
		delay = queue.delay
	}
	if maxsize == UnsetMaxsize {
		maxsize = queue.maxsize
	}

	if err := validateVt(vt); err != nil {
		return nil, err
	}
	if err := validateDelay(delay); err != nil {
		return nil, err
	}
	if err := validateMaxsize(maxsize); err != nil {
		return nil, err
	}

	key := rsmq.ns + qname + q

	tx := rsmq.client.TxPipeline()
	tx.HSet(key, "modified", queue.ts)
	tx.HSet(key, "vt", vt)
	tx.HSet(key, "delay", delay)
	tx.HSet(key, "maxsize", maxsize)
	if _, err := tx.Exec(); err != nil {
		return nil, err
	}

	return rsmq.GetQueueAttributes(qname)
}

// Quit closes redis client
func (rsmq *RedisSMQ) Quit() error {
	return rsmq.client.Close()
}

// DeleteQueue deletes queue
func (rsmq *RedisSMQ) DeleteQueue(qname string) error {
	if err := validateQname(qname); err != nil {
		return err
	}

	key := rsmq.ns + qname

	tx := rsmq.client.TxPipeline()
	r := tx.Del(key + q)
	tx.Del(key)
	tx.SRem(rsmq.ns+queues, qname)
	if _, err := tx.Exec(); err != nil {
		return nil
	}
	if r.Val() == 0 {
		return ErrQueueNotFound
	}

	return nil
}

// SendMessage sends a message to the queue.
// If a custom ID is provided via WithMessageID option:
// - The message will use that ID instead of generating a new one
// - If a message with that ID already exists, it will be overridden
// - The msg.Sent timestamp will not reflect the actual send time for overridden messages
// - Message timing is controlled by the delay parameter, not by the ID's timestamp
func (rsmq *RedisSMQ) SendMessage(qname string, message string, delay uint, opts ...SendMessageOption) (string, error) {
	if err := validateQname(qname); err != nil {
		return "", err
	}

	options := &sendMessageOptions{}
	for _, opt := range opts {
		opt(options)
	}

	queue, err := rsmq.getQueue(qname, options.id == "")
	if err != nil {
		return "", err
	}

	if delay == UnsetDelay {
		delay = queue.delay
	}

	if err := validateDelay(delay); err != nil {
		return "", err
	}

	if queue.maxsize != -1 && len(message) > queue.maxsize {
		return "", ErrMessageTooLong
	}

	key := rsmq.ns + qname

	// Use custom ID if provided, otherwise use generated one
	messageID := queue.uid
	if options.id != "" {
		if err := validateID(options.id); err != nil {
			return "", err
		}
		messageID = options.id
	}

	tx := rsmq.client.TxPipeline()
	tx.ZAdd(key, redis.Z{
		Score:  float64(queue.ts + uint64(delay)*1000),
		Member: messageID,
	})
	tx.HSet(key+q, messageID, message)
	tx.HIncrBy(key+q, "totalsent", 1)
	if _, err := tx.Exec(); err != nil {
		return "", err
	}

	return messageID, nil
}

// ReceiveMessage receives message from the queue
func (rsmq *RedisSMQ) ReceiveMessage(qname string, vt uint) (*QueueMessage, error) {
	if err := validateQname(qname); err != nil {
		return nil, err
	}

	queue, err := rsmq.getQueue(qname, true)
	if err != nil {
		return nil, err
	}

	if vt == UnsetVt {
		vt = queue.vt
	}

	if err := validateVt(vt); err != nil {
		return nil, err
	}

	key := rsmq.ns + qname

	qvt := strconv.FormatUint(queue.ts+uint64(vt)*1000, 10)
	ct := strconv.FormatUint(queue.ts, 10)

	evalCmd := rsmq.client.EvalSha(hashReceiveMessage, []string{key}, ct, qvt)
	return rsmq.createQueueMessage(evalCmd)
}

// PopMessage pop message from queue
func (rsmq *RedisSMQ) PopMessage(qname string) (*QueueMessage, error) {
	if err := validateQname(qname); err != nil {
		return nil, err
	}

	queue, err := rsmq.getQueue(qname, false)
	if err != nil {
		return nil, err
	}

	key := rsmq.ns + qname

	t := strconv.FormatUint(queue.ts, 10)

	evalCmd := rsmq.client.EvalSha(hashPopMessage, []string{key}, t)
	return rsmq.createQueueMessage(evalCmd)
}

func (rsmq *RedisSMQ) createQueueMessage(cmd *redis.Cmd) (*QueueMessage, error) {
	val := cmd.Val()
	
	// Try different type assertions for cluster vs regular client compatibility
	var vals []any
	if v, ok := val.([]any); ok {
		vals = v
	} else if v, ok := val.([]interface{}); ok {
		vals = make([]any, len(v))
		for i, item := range v {
			vals[i] = item
		}
	} else {
		return nil, fmt.Errorf("mismatched message response type: got %T, value: %v", val, val)
	}
	if len(vals) == 0 {
		return nil, nil
	}
	id := vals[0].(string)
	message := vals[1].(string)
	rc := convertIntToUint(vals[2])
	fr := convertStringToInt[int64](vals[3])
	sent, err := strconv.ParseInt(id[0:10], 36, 64)
	if err != nil {
		panic(err)
	}

	return &QueueMessage{
		ID:      id,
		Message: message,
		Rc:      rc,
		Fr:      time.UnixMilli(fr),
		Sent:    time.UnixMicro(sent),
	}, nil
}

// ChangeMessageVisibility changes message visibility
// to refer queue vt
//
//	err:=redisRsmq.ChangeMessageVisibility(qname,id,rsmq.UnsetVt)
func (rsmq *RedisSMQ) ChangeMessageVisibility(qname string, id string, vt uint) error {
	if err := validateQname(qname); err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}

	queue, err := rsmq.getQueue(qname, false)
	if err != nil {
		return err
	}

	if vt == UnsetVt {
		vt = queue.vt
	}

	if err := validateVt(vt); err != nil {
		return err
	}

	key := rsmq.ns + qname
	t := strconv.FormatUint(queue.ts+uint64(vt)*1000, 10)

	evalCmd := rsmq.client.EvalSha(hashChangeMessageVisibility, []string{key}, id, t)
	if e, err := evalCmd.Bool(); err != nil {
		return err
	} else if !e {
		return ErrMessageNotFound
	}

	return nil
}

// DeleteMessage deletes message in queue
func (rsmq *RedisSMQ) DeleteMessage(qname string, id string) error {
	if err := validateQname(qname); err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}

	_, err := rsmq.getQueue(qname, false)
	if err != nil {
		return err
	}

	key := rsmq.ns + qname

	tx := rsmq.client.TxPipeline()
	zremIntCmd := tx.ZRem(key, id)
	hdelIntCmd := tx.HDel(key+q, id, id+":rc", id+":fr")
	if _, err := tx.Exec(); err != nil {
		return err
	}

	if zremIntCmd.Val() != 1 || hdelIntCmd.Val() == 0 {
		return ErrMessageNotFound
	}

	return nil
}
