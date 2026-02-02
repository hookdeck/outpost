package rsmq

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// RSMQSuite runs RSMQ tests against different backends.
type RSMQSuite struct {
	suite.Suite
	cfg    *redis.RedisConfig
	client RedisClient
	rsmq   *RedisSMQ
}

type RedisRSMQSuite struct{ RSMQSuite }
type DragonflyRSMQSuite struct{ RSMQSuite }

// TestRedisRSMQSuite is skipped by default - run with TESTFULL=1 for full compatibility testing
func TestRedisRSMQSuite(t *testing.T) {
	testutil.SkipUnlessCompat(t)
	suite.Run(t, new(RedisRSMQSuite))
}

func TestDragonflyRSMQSuite(t *testing.T) { suite.Run(t, new(DragonflyRSMQSuite)) }

func (s *RedisRSMQSuite) SetupSuite() {
	testinfra.Start(s.T())
	s.cfg = testinfra.NewRedisConfig(s.T())
}

func (s *RedisRSMQSuite) SetupTest() {
	// Flush the container's DB 0 before each test method for a clean state.
	flushClient, err := redis.New(context.Background(), s.cfg)
	if err != nil {
		s.T().Fatalf("failed to create redis client for flush: %v", err)
	}
	flushClient.FlushDB(context.Background())
	flushClient.Close()

	client, err := redis.New(context.Background(), s.cfg)
	if err != nil {
		s.T().Fatalf("failed to create redis client: %v", err)
	}
	s.T().Cleanup(func() { client.Close() })
	s.client = NewRedisAdapter(client)
	s.rsmq = NewRedisSMQ(s.client, "test")
}

func (s *DragonflyRSMQSuite) SetupSuite() {
	testinfra.Start(s.T())
	s.cfg = testinfra.NewDragonflyConfig(s.T())
}

func (s *DragonflyRSMQSuite) SetupTest() {
	// Flush the container's DB 0 before each test method for a clean state.
	flushClient, err := redis.New(context.Background(), s.cfg)
	if err != nil {
		s.T().Fatalf("failed to create redis client for flush: %v", err)
	}
	flushClient.FlushDB(context.Background())
	flushClient.Close()

	client, err := redis.New(context.Background(), s.cfg)
	if err != nil {
		s.T().Fatalf("failed to create redis client: %v", err)
	}
	s.T().Cleanup(func() { client.Close() })
	s.client = NewRedisAdapter(client)
	s.rsmq = NewRedisSMQ(s.client, "test")
}

func (s *RSMQSuite) TestNewRedisSMQ() {
	t := s.T()
	ns := "test"

	rsmq := NewRedisSMQ(s.client, ns)
	assert.NotNil(t, rsmq, "rsmq is nil")
	assert.NotNil(t, rsmq.client, "client in rsmq is nil")
	assert.Equal(t, ns+":", rsmq.ns, "namespace is not as expected")

	t.Run("client with empty namespace", func(t *testing.T) {
		rsmq := NewRedisSMQ(s.client, "")
		assert.NotNil(t, rsmq, "rsmq is nil")
		assert.Equal(t, defaultNs+":", rsmq.ns, "namespace is not as expected")
	})

	t.Run("panic when redis client is nil", func(t *testing.T) {
		assert.Panics(t, func() {
			NewRedisSMQ(nil, ns)
		}, "not panicking when redis client is not")
	})
}

func (s *RSMQSuite) TestCreateQueue() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	t.Run("error when the queue already exists", func(t *testing.T) {
		err = s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.NotNil(t, err, "error is nil on creating the existing queue")
		assert.Equal(t, ErrQueueExists, err, "error is not as expected")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		err = s.rsmq.CreateQueue("it is invalid queue name", UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.NotNil(t, err, "error is nil on creating a queue with invalid name")
	})

	t.Run("error when the queue attribute vt is not valid", func(t *testing.T) {
		err = s.rsmq.CreateQueue("queue-invalid-vt", 10_000_000, UnsetDelay, UnsetMaxsize)
		assert.NotNil(t, err, "error is nil on creating a queue with invalid vt")
		assert.Equal(t, ErrInvalidVt, err, "error is not as expected")
	})

	t.Run("error when the queue attribute delay is not valid", func(t *testing.T) {
		err = s.rsmq.CreateQueue("queue-invalid-delay", UnsetVt, 10_000_000, UnsetMaxsize)
		assert.NotNil(t, err, "error is nil on creating a queue with invalid delay")
		assert.Equal(t, ErrInvalidDelay, err, "error is not as expected")
	})

	t.Run("error when the queue attribute maxsize is not valid", func(t *testing.T) {
		err = s.rsmq.CreateQueue("queue-invalid-maxsize", UnsetVt, UnsetDelay, 1023)
		assert.NotNil(t, err, "error is nil on creating a queue with invalid maxsize")
		assert.Equal(t, ErrInvalidMaxsize, err, "error is not as expected")

		err = s.rsmq.CreateQueue("queue-invalid-maxsize", UnsetVt, UnsetDelay, 65537)
		assert.NotNil(t, err, "error is nil on creating a queue with invalid maxsize")
		assert.Equal(t, ErrInvalidMaxsize, err, "error is not as expected")
	})
}

func (s *RSMQSuite) TestListQueues() {
	t := s.T()
	qname1 := "que1"
	qname2 := "que2"
	qname3 := "que3"

	err := s.rsmq.CreateQueue(qname1, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	err = s.rsmq.CreateQueue(qname2, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	err = s.rsmq.CreateQueue(qname3, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	queues, err := s.rsmq.ListQueues()
	assert.Nil(t, err, "error is not nil on listing queues")
	assert.Len(t, queues, 3, "queues length is not as expected")
	assert.Contains(t, queues, qname1)
	assert.Contains(t, queues, qname2)
	assert.Contains(t, queues, qname3)
}

func (s *RSMQSuite) TestGetQueueAttributes() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	queAttrib, err := s.rsmq.GetQueueAttributes(qname)
	assert.Nil(t, err, "error is not nil on getting queue attributes")
	assert.NotNil(t, queAttrib, "queueAttributes is nil")
	assert.EqualValues(t, defaultVt, queAttrib.Vt, "queueAttributes vt is not as expected")
	assert.EqualValues(t, defaultDelay, queAttrib.Delay, "queueAttributes delay is not as expected")
	assert.Equal(t, defaultMaxsize, queAttrib.Maxsize, "queueAttributes maxsize is not as expected")
	assert.Zero(t, queAttrib.TotalRecv, "queueAttributes totalRecv is not zero")
	assert.Zero(t, queAttrib.TotalSent, "queueAttributes totalSent is not zero")
	assert.NotZero(t, queAttrib.Created, "queueAttributes created is zero")
	assert.NotZero(t, queAttrib.Modified, "queueAttributes modified is zero")
	assert.Equal(t, queAttrib.Created, queAttrib.Modified, "queueAttributes created is not equal to modified")
	assert.Zero(t, queAttrib.Msgs, "queueAttributes msgs is not zero")
	assert.Zero(t, queAttrib.HiddenMsgs, "queueAttributes hiddenMsgs is not zero")

	t.Run("attributes of the queue with custom configs", func(t *testing.T) {
		qname := "queue-custom-config"

		vt := uint(90)
		delay := uint(30)
		maxsize := 2048

		err := s.rsmq.CreateQueue(qname, vt, delay, maxsize)
		assert.Nil(t, err, "error is not nil on creating a queue")

		queAttrib, err := s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.Equal(t, vt, queAttrib.Vt, "queueAttributes vt is not as expected")
		assert.Equal(t, delay, queAttrib.Delay, "queueAttributes delay is not as expected")
		assert.Equal(t, maxsize, queAttrib.Maxsize, "queueAttributes maxsize is not as expected")
	})

	t.Run("attributes after sending, receiving and pop messages", func(t *testing.T) {
		_, err := s.rsmq.SendMessage(qname, "msg-1", UnsetDelay)
		assert.Nil(t, err, "error is not nil on sending message to the queue")
		queAttrib, err := s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.Zero(t, queAttrib.TotalRecv, "queueAttributes totalRecv is not zero")
		assert.EqualValues(t, 1, queAttrib.TotalSent, "queueAttributes totalSent is not as expected")
		assert.EqualValues(t, 1, queAttrib.Msgs, "queueAttributes msg is not as expected")

		_, err = s.rsmq.SendMessage(qname, "msg-2", UnsetDelay)
		assert.Nil(t, err, "error is not nil on sending message to the queue")
		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.Zero(t, queAttrib.TotalRecv, "queueAttributes totalRecv is not zero")
		assert.EqualValues(t, 2, queAttrib.TotalSent, "queueAttributes totalSent is not as expected")
		assert.EqualValues(t, 2, queAttrib.Msgs, "queueAttributes msg is not as expected")

		_, err = s.rsmq.ReceiveMessage(qname, UnsetVt)
		assert.Nil(t, err, "error is not nil on receiving message from the queue")
		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.EqualValues(t, 1, queAttrib.TotalRecv, "queueAttributes totalRecv is not as expected")
		assert.EqualValues(t, 2, queAttrib.TotalSent, "queueAttributes totalSent is not as expected")
		assert.EqualValues(t, 2, queAttrib.Msgs, "queueAttributes msg is not as expected")

		_, err = s.rsmq.PopMessage(qname)
		assert.Nil(t, err, "error is not nil on pop message from the queue")
		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.EqualValues(t, 2, queAttrib.TotalRecv, "queueAttributes totalRecv is not as expected")
		assert.EqualValues(t, 2, queAttrib.TotalSent, "queueAttributes totalSent is not as expected")
		assert.EqualValues(t, 1, queAttrib.Msgs, "queueAttributes msg is not as expected")
	})

	t.Run("error when the queue does not exist", func(t *testing.T) {
		queAttrib, err = s.rsmq.GetQueueAttributes("non-existing")
		assert.NotNil(t, err, "error is nil on getting attributes of non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
		assert.Nil(t, queAttrib, "queueAttributes is not nil on getting attributes of non-existing queue")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		queAttrib, err = s.rsmq.GetQueueAttributes("it is invalid queue name")
		assert.NotNil(t, err, "error is nil on getting attributes of queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
		assert.Nil(t, queAttrib, "queueAttributes is not nil on getting attributes of queue with invalid name")
	})
}

func (s *RSMQSuite) TestSetQueueAttributes() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	newVt := uint(90)
	newDelay := uint(30)
	newMaxsize := 2048

	queAttrib, err := s.rsmq.SetQueueAttributes(qname, newVt, newDelay, newMaxsize)
	assert.Nil(t, err, "error is not nil on setting queue attributes")
	assert.NotNil(t, queAttrib, "queueAttributes is nil")

	queAttrib, err = s.rsmq.GetQueueAttributes(qname)
	assert.Nil(t, err, "error is not nil on getting queue attributes")
	assert.NotNil(t, queAttrib, "queueAttributes is nil")
	assert.Equal(t, newVt, queAttrib.Vt, "queueAttributes vt is not as expected")
	assert.Equal(t, newDelay, queAttrib.Delay, "queueAttributes delay is not as expected")
	assert.Equal(t, newMaxsize, queAttrib.Maxsize, "queueAttributes maxsize is not as expected")
	assert.True(t, queAttrib.Modified > queAttrib.Created, "queueAttributes modified is not greater than created")

	t.Run("error when the queue does not exist", func(t *testing.T) {
		queAttrib, err = s.rsmq.SetQueueAttributes("non-existing", UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.NotNil(t, err, "error is nil on setting attributes of non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
		assert.Nil(t, queAttrib, "queueAttributes is not nil on setting attributes of non-existing queue")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		queAttrib, err = s.rsmq.SetQueueAttributes("it is invalid queue name", UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.NotNil(t, err, "error is nil on setting attributes of queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
		assert.Nil(t, queAttrib, "queueAttributes is not nil on setting attributes of queue with invalid name")
	})

	t.Run("error when the queue attribute vt is not valid", func(t *testing.T) {
		qname := "queue-set-invalid-vt"
		err = s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.Nil(t, err, "error is not nil on creating a queue")

		queAttrib, err := s.rsmq.SetQueueAttributes(qname, 10_000_000, UnsetDelay, UnsetMaxsize)
		assert.NotNil(t, err, "error is not nil on setting invalid queue attribute vt")
		assert.Nil(t, queAttrib, "queAttrib is not nil on setting invalid queue attribute vt")

		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.EqualValues(t, defaultVt, queAttrib.Vt, "queueAttributes vt is not as expected")
	})

	t.Run("error when the queue attribute delay is not valid", func(t *testing.T) {
		qname := "queue-set-invalid-delay"
		err = s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.Nil(t, err, "error is not nil on creating a queue")

		queAttrib, err := s.rsmq.SetQueueAttributes(qname, UnsetVt, 10_000_000, UnsetMaxsize)
		assert.NotNil(t, err, "error is not nil on setting invalid queue attribute delay")
		assert.Nil(t, queAttrib, "queAttrib is not nil on setting invalid queue attribute delay")

		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.EqualValues(t, defaultDelay, queAttrib.Delay, "queueAttributes delay is not as expected")
	})

	t.Run("error when the queue attribute maxsize is not valid", func(t *testing.T) {
		qname := "queue-set-invalid-maxsize"
		err = s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		assert.Nil(t, err, "error is not nil on creating a queue")

		queAttrib, err := s.rsmq.SetQueueAttributes(qname, UnsetVt, UnsetDelay, 1023)
		assert.NotNil(t, err, "error is not nil on setting invalid queue attribute maxsize")
		assert.Nil(t, queAttrib, "queAttrib is not nil on setting invalid queue attribute maxsize")

		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.Equal(t, defaultMaxsize, queAttrib.Maxsize, "queueAttributes maxsize is not as expected")

		queAttrib, err = s.rsmq.SetQueueAttributes(qname, UnsetVt, UnsetDelay, 65537)
		assert.NotNil(t, err, "error is not nil on setting invalid queue attribute maxsize")
		assert.Nil(t, queAttrib, "queAttrib is not nil on setting invalid queue attribute maxsize")

		queAttrib, err = s.rsmq.GetQueueAttributes(qname)
		assert.Nil(t, err, "error is not nil on getting queue attributes")
		assert.NotNil(t, queAttrib, "queueAttributes is nil")
		assert.Equal(t, defaultMaxsize, queAttrib.Maxsize, "queueAttributes maxsize is not as expected")
	})
}

func (s *RSMQSuite) TestQuit() {
	t := s.T()
	err := s.rsmq.Quit()
	assert.Nil(t, err, "error is not nil on quit")
}

func (s *RSMQSuite) TestDeleteQueue() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	err = s.rsmq.DeleteQueue(qname)
	assert.Nil(t, err, "error is not nil on deleting the queue")

	queues, err := s.rsmq.ListQueues()
	assert.Nil(t, err, "error is not nil on listing queues")
	assert.Empty(t, queues, "queue slice is not empty")

	t.Run("error when the queue does not exist", func(t *testing.T) {
		err = s.rsmq.DeleteQueue("non-existing")
		assert.NotNil(t, err, "error is nil on deleting non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		err = s.rsmq.DeleteQueue("it is invalid queue name")
		assert.NotNil(t, err, "error is nil on deleting queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
	})
}

func (s *RSMQSuite) TestSendMessage() {
	t := s.T()
	qname1 := "que1"

	err := s.rsmq.CreateQueue(qname1, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	message := "message"

	id, err := s.rsmq.SendMessage(qname1, message, UnsetDelay)
	assert.Nil(t, err, "error is not nil on sending a message")
	assert.NotEmpty(t, id, "id is empty on sending a message")

	t.Run("error when the message size limit is exceeded", func(t *testing.T) {
		qname2 := "que2"
		err = s.rsmq.CreateQueue(qname2, UnsetVt, UnsetDelay, 1024)
		assert.Nil(t, err, "error is not nil on creating a queue")

		b := make([]byte, 2048)
		message = string(b)
		id, err = s.rsmq.SendMessage(qname2, message, UnsetDelay)
		assert.NotNil(t, err, "error is nil on sending a message which exceeds the size limit")
		assert.Equal(t, ErrMessageTooLong, err, "error is not as expected")
		assert.Empty(t, id, "id is not empty on sending a message which exceeds the size limit")
	})

	t.Run("error when the queue does not exist", func(t *testing.T) {
		id, err = s.rsmq.SendMessage("non-existing", message, UnsetDelay)
		assert.NotNil(t, err, "error is nil on sending a message to non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
		assert.Empty(t, id, "id is not empty on sending a message to non-existing queue")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		id, err = s.rsmq.SendMessage("it is invalid queue name", message, UnsetDelay)
		assert.NotNil(t, err, "error is nil on sending a message to the queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
		assert.Empty(t, id, "id is not empty on sending a message to the queue with invalid name")
	})

	t.Run("error when delay is not valid", func(t *testing.T) {
		id, err := s.rsmq.SendMessage(qname1, message, 10_000_000)
		assert.NotNil(t, err, "error is nil on sending with invalid delay")
		assert.Equal(t, ErrInvalidDelay, err, "error is not as expected")
		assert.Empty(t, id, "id is not empty on sending a message with invalid delay")
	})
}

func (s *RSMQSuite) TestReceiveMessage() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	message := "message"
	id, err := s.rsmq.SendMessage(qname, message, UnsetDelay)
	assert.Nil(t, err, "error is not nil on sending a message")
	assert.NotEmpty(t, id, "id is empty on sending a message")

	queMsg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
	assert.Nil(t, err, "error is not nil on receiving the message")
	assert.NotNil(t, queMsg, "queueMessage is nil")
	assert.Equal(t, id, queMsg.ID, "queueMessage ID is not as expected")
	assert.Equal(t, message, queMsg.Message, "queueMessage Message is not as expected")

	t.Run("no error when the queue is empty", func(t *testing.T) {
		queMsg, err = s.rsmq.ReceiveMessage(qname, UnsetVt)
		assert.Nil(t, err, "error is not nil on receiving a message from empty queue")
		assert.Nil(t, queMsg, "queueMessage is not nil on receiving a message from empty queue")
	})

	t.Run("error when the queue does not exist", func(t *testing.T) {
		queMsg, err = s.rsmq.ReceiveMessage("non-existing", UnsetVt)
		assert.NotNil(t, err, "error is nil on receiving the message from non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
		assert.Empty(t, queMsg, "queueMessage is not empty on receiving the message from non-existing queue")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		queMsg, err = s.rsmq.ReceiveMessage("it is invalid queue name", UnsetVt)
		assert.NotNil(t, err, "error is nil on receiving the message from the queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
		assert.Empty(t, queMsg, "queueMessage is not empty on receiving the message from the queue with invalid name")
	})

	t.Run("error when vt is not valid", func(t *testing.T) {
		queMsg, err := s.rsmq.ReceiveMessage(qname, 10_000_000)
		assert.NotNil(t, err, "error is nil on receiving the message with invalid vt")
		assert.Equal(t, ErrInvalidVt, err, "error is not as expected")
		assert.Nil(t, queMsg, "queueMessage is not nil when the vt is not valid")
	})
}

func (s *RSMQSuite) TestPopMessage() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	message := "message"
	id, err := s.rsmq.SendMessage(qname, message, UnsetDelay)
	assert.Nil(t, err, "error is not nil on sending a message")
	assert.NotEmpty(t, id, "id is empty on sending a message")

	queMsg, err := s.rsmq.PopMessage(qname)
	assert.Nil(t, err, "error is not nil on pop the message")
	assert.NotNil(t, queMsg, "queueMessage is nil")
	assert.Equal(t, id, queMsg.ID, "queueMessage ID is not as expected")
	assert.Equal(t, message, queMsg.Message, "queueMessage Message is not as expected")

	t.Run("no error when the queue is empty", func(t *testing.T) {
		queMsg, err = s.rsmq.PopMessage(qname)
		assert.Nil(t, err, "error is not nil on pop a message from empty queue")
		assert.Nil(t, queMsg, "queueMessage is not nil on pop a message from empty queue")
	})

	t.Run("error when the queue does not exist", func(t *testing.T) {
		queMsg, err = s.rsmq.PopMessage("non-existing")
		assert.NotNil(t, err, "error is nil on pop the message from non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
		assert.Empty(t, queMsg, "queueMessage is not empty on pop the message from non-existing queue")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		queMsg, err = s.rsmq.PopMessage("it is invalid queue name")
		assert.NotNil(t, err, "error is nil on pop the message from the queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
		assert.Empty(t, queMsg, "queueMessage is not empty on pop the message from the queue with invalid name")
	})
}

func (s *RSMQSuite) TestChangeMessageVisibility() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	message := "message"
	id, err := s.rsmq.SendMessage(qname, message, UnsetDelay)
	assert.Nil(t, err, "error is not nil on sending a message")
	assert.NotEmpty(t, id, "id is empty on sending a message")

	newVt := uint(0)
	err = s.rsmq.ChangeMessageVisibility(qname, id, newVt)
	assert.Nil(t, err, "error is not nil on changing the message visibility")

	queMsg, err := s.rsmq.PopMessage(qname)
	assert.Nil(t, err, "error is not nil on pop the message")
	assert.NotNil(t, queMsg, "queueMessage is nil")
	assert.Equal(t, id, queMsg.ID, "queueMessage ID is not as expected")
	assert.Equal(t, message, queMsg.Message, "queueMessage Message is not as expected")

	t.Run("error when the message does not exist", func(t *testing.T) {
		err = s.rsmq.ChangeMessageVisibility(qname, id, UnsetVt)
		assert.NotNil(t, err, "error is nil on changing the visibility of message of non-existing message")
		assert.Equal(t, ErrMessageNotFound, err, "error is not as expected")
	})

	t.Run("error when the queue does not exist", func(t *testing.T) {
		err = s.rsmq.ChangeMessageVisibility("non-existing", id, UnsetVt)
		assert.NotNil(t, err, "error is nil on changing the visibility of a message in non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		err = s.rsmq.ChangeMessageVisibility("it is invalid queue name", id, UnsetVt)
		assert.NotNil(t, err, "error is nil on changing the visibility of a message in the queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
	})

	t.Run("error when the message id is not valid", func(t *testing.T) {
		err = s.rsmq.ChangeMessageVisibility(qname, "invalid message id", UnsetVt)
		assert.NotNil(t, err, "error is nil on changing the visibility of a message with invalid id")
		assert.Equal(t, ErrInvalidID, err, "error is not as expected")
	})

	t.Run("error when vt is not valid", func(t *testing.T) {
		message := "message"
		id, err := s.rsmq.SendMessage(qname, message, UnsetDelay)
		assert.Nil(t, err, "error is not nil on sending a message")
		assert.NotEmpty(t, id, "id is empty on sending a message")

		newVt := uint(10_000_000)
		err = s.rsmq.ChangeMessageVisibility(qname, id, newVt)
		assert.NotNil(t, ErrInvalidVt, err, "error is nil on changing the message visibility with invalid vt")

		queMsg, err := s.rsmq.PopMessage(qname)
		assert.Nil(t, err, "error is not nil on pop the message")
		assert.NotNil(t, queMsg, "queueMessage is nil")
		assert.Equal(t, id, queMsg.ID, "queueMessage ID is not as expected")
		assert.Equal(t, message, queMsg.Message, "queueMessage Message is not as expected")
	})
}

func (s *RSMQSuite) TestDeleteMessage() {
	t := s.T()
	qname := "que"

	err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
	assert.Nil(t, err, "error is not nil on creating a queue")

	message := "message"
	id, err := s.rsmq.SendMessage(qname, message, UnsetVt)
	assert.Nil(t, err, "error is not nil on sending a message")
	assert.NotEmpty(t, id, "id is empty on sending a message")

	err = s.rsmq.DeleteMessage(qname, id)
	assert.Nil(t, err, "error is not nil on deleting the message")

	queMsg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
	assert.Nil(t, err, "error is not nil on receiving a message from empty queue")
	assert.Nil(t, queMsg, "queueMessage is not nil on receiving a message from empty queue")

	t.Run("error when the message does not exist", func(t *testing.T) {
		err = s.rsmq.DeleteMessage(qname, id)
		assert.NotNil(t, err, "error is nil on deleting non-existing message")
		assert.Equal(t, ErrMessageNotFound, err, "error is not as expected")
	})

	t.Run("error when the queue does not exist", func(t *testing.T) {
		err = s.rsmq.DeleteMessage("non-existing", id)
		assert.NotNil(t, err, "error is nil on deleting a message in non-existing queue")
		assert.Equal(t, ErrQueueNotFound, err, "error is not as expected")
	})

	t.Run("error when the queue name is not valid", func(t *testing.T) {
		err = s.rsmq.DeleteMessage("it is invalid queue name", id)
		assert.NotNil(t, err, "error is nil on deleting a message in the queue with invalid name")
		assert.Equal(t, ErrInvalidQname, err, "error is not as expected")
	})

	t.Run("error when the message id is not valid", func(t *testing.T) {
		err = s.rsmq.DeleteMessage(qname, "invalid message id")
		assert.NotNil(t, err, "error is nil on deleting a message with invalid id")
		assert.Equal(t, ErrInvalidID, err, "error is not as expected")
	})
}

func (s *RSMQSuite) TestSendMessageWithCustomID() {
	t := s.T()
	qname := "myqueue"
	message := "test message"
	// Format: 10 chars base36 timestamp + 22 chars alphanumeric
	customID := "kf12mn5ui9" + "ABCDEFGHIJKLMNOPQRSTUV"

	t.Run("success with custom ID", func(t *testing.T) {
		err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.rsmq.DeleteQueue(qname)

		id, err := s.rsmq.SendMessage(qname, message, UnsetDelay, WithMessageID(customID))
		if err != nil {
			t.Fatal(err)
		}
		if id != customID {
			t.Errorf("expected message ID %s, got %s", customID, id)
		}

		// Verify message can be received
		msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
		if err != nil {
			t.Fatal(err)
		}
		if msg.ID != customID {
			t.Errorf("expected received message ID %s, got %s", customID, msg.ID)
		}
		if msg.Message != message {
			t.Errorf("expected message content %s, got %s", message, msg.Message)
		}
	})

	t.Run("error with invalid custom ID", func(t *testing.T) {
		err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.rsmq.DeleteQueue(qname)

		_, err = s.rsmq.SendMessage(qname, message, UnsetDelay, WithMessageID("invalid"))
		if err == nil {
			t.Error("expected error for invalid message ID")
		}
		if err != ErrInvalidID {
			t.Errorf("expected error %v, got %v", ErrInvalidID, err)
		}
	})

	t.Run("backward compatibility without custom ID", func(t *testing.T) {
		err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.rsmq.DeleteQueue(qname)

		id, err := s.rsmq.SendMessage(qname, message, UnsetDelay)
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 32 {
			t.Errorf("expected generated ID length 32, got %d", len(id))
		}
	})

	t.Run("duplicate message ID overrides previous message", func(t *testing.T) {
		err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.rsmq.DeleteQueue(qname)

		// Send first message
		firstMessage := "first message"
		_, err = s.rsmq.SendMessage(qname, firstMessage, UnsetDelay, WithMessageID(customID))
		if err != nil {
			t.Fatal(err)
		}

		// Send second message with same ID
		secondMessage := "second message"
		_, err = s.rsmq.SendMessage(qname, secondMessage, UnsetDelay, WithMessageID(customID))
		if err != nil {
			t.Fatal(err)
		}

		// Verify only the second message exists and is receivable
		msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
		if err != nil {
			t.Fatal(err)
		}
		if msg == nil {
			t.Fatal("expected to receive a message")
		}
		if msg.ID != customID {
			t.Errorf("expected message ID %s, got %s", customID, msg.ID)
		}
		if msg.Message != secondMessage {
			t.Errorf("expected message content %s, got %s", secondMessage, msg.Message)
		}

		// Verify no more messages exist
		msg, err = s.rsmq.ReceiveMessage(qname, UnsetVt)
		if err != nil {
			t.Fatal(err)
		}
		if msg != nil {
			t.Error("expected no more messages, but received one")
		}
	})

	t.Run("override changes delay timing", func(t *testing.T) {
		err := s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.rsmq.DeleteQueue(qname)

		// Schedule first message for 1s
		firstMessage := "first message"
		_, err = s.rsmq.SendMessage(qname, firstMessage, 1, WithMessageID(customID))
		if err != nil {
			t.Fatal(err)
		}

		// Override with second message for 2s
		secondMessage := "second message"
		_, err = s.rsmq.SendMessage(qname, secondMessage, 2, WithMessageID(customID))
		if err != nil {
			t.Fatal(err)
		}

		// After 1s, no message should be available
		time.Sleep(time.Second)
		msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
		if err != nil {
			t.Fatal(err)
		}
		if msg != nil {
			t.Error("expected no message after 1s, but received one")
		}

		// After another 1s (total 2s), message should be available
		time.Sleep(time.Second)
		msg, err = s.rsmq.ReceiveMessage(qname, UnsetVt)
		if err != nil {
			t.Fatal(err)
		}
		if msg == nil {
			t.Fatal("expected to receive a message after 2s")
		}
		if msg.ID != customID {
			t.Errorf("expected message ID %s, got %s", customID, msg.ID)
		}
		if msg.Message != secondMessage {
			t.Errorf("expected message content %s, got %s", secondMessage, msg.Message)
		}
	})
}
