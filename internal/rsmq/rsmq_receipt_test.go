package rsmq

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s *RSMQSuite) receiptFixture(qname, id string) string {
	t := s.T()
	require.NoError(t, s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize))
	_, err := s.rsmq.SendMessage(qname, "old", 0, WithMessageID(id))
	require.NoError(t, err)
	msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.NotEmpty(t, msg.Receipt)
	return msg.Receipt
}

func (s *RSMQSuite) hashField(qname, field string) interface{} {
	vals, err := s.client.HMGet("test:"+qname+":Q", field).Result()
	require.NoError(s.T(), err)
	return vals[0]
}

const receiptTestID = "0000000000abcdefghijklmnopqrst12"

func (s *RSMQSuite) TestReceiptRotatesPerReceive() {
	qname := "que"
	r1 := s.receiptFixture(qname, receiptTestID)

	require.NoError(s.T(), s.rsmq.ChangeMessageVisibilityWithReceipt(qname, receiptTestID, r1, 0))
	msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), msg)
	assert.NotEmpty(s.T(), msg.Receipt)
	assert.NotEqual(s.T(), r1, msg.Receipt)
}

func (s *RSMQSuite) TestDeleteMessageWithReceipt() {
	t := s.T()
	qname := "que"
	id := receiptTestID

	t.Run("deletes with the current receipt", func(t *testing.T) {
		r := s.receiptFixture(qname, id)
		require.NoError(t, s.rsmq.DeleteMessageWithReceipt(qname, id, r))
		assert.Nil(t, s.hashField(qname, id))
		assert.Nil(t, s.hashField(qname, id+":rt"))
		assert.Equal(t, ErrMessageNotFound, s.rsmq.DeleteMessageWithReceipt(qname, id, r))
	})

	t.Run("keeps a message re-created under the same ID", func(t *testing.T) {
		_, err := s.rsmq.SendMessage(qname, "old", 0, WithMessageID(id))
		require.NoError(t, err)
		msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
		require.NoError(t, err)
		require.NotNil(t, msg)
		stale := msg.Receipt

		_, err = s.rsmq.SendMessage(qname, "new", 0, WithMessageID(id))
		require.NoError(t, err)

		assert.Equal(t, ErrReceiptMismatch, s.rsmq.DeleteMessageWithReceipt(qname, id, stale))

		msg, err = s.rsmq.ReceiveMessage(qname, UnsetVt)
		require.NoError(t, err)
		require.NotNil(t, msg)
		assert.Equal(t, "new", msg.Message)
		assert.EqualValues(t, 1, msg.Rc)

		require.NoError(t, s.rsmq.DeleteMessageWithReceipt(qname, id, msg.Receipt))
		assert.Nil(t, s.hashField(qname, id+":rt"))
		next, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
		require.NoError(t, err)
		assert.Nil(t, next)
	})

	t.Run("not found and empty receipt", func(t *testing.T) {
		other := "0000000000abcdefghijklmnopqrst13"
		assert.Equal(t, ErrMessageNotFound, s.rsmq.DeleteMessageWithReceipt(qname, other, "1"))
		assert.Equal(t, ErrReceiptMismatch, s.rsmq.DeleteMessageWithReceipt(qname, other, ""))
	})
}

func (s *RSMQSuite) TestChangeMessageVisibilityWithReceipt() {
	t := s.T()
	qname := "que"
	id := receiptTestID
	stale := s.receiptFixture(qname, id)

	_, err := s.rsmq.SendMessage(qname, "new", 0, WithMessageID(id))
	require.NoError(t, err)

	assert.Equal(t, ErrReceiptMismatch, s.rsmq.ChangeMessageVisibilityWithReceipt(qname, id, stale, 60))

	msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
	require.NoError(t, err)
	require.NotNil(t, msg, "new message should still be visible")
	assert.Equal(t, "new", msg.Message)

	require.NoError(t, s.rsmq.ChangeMessageVisibilityWithReceipt(qname, id, msg.Receipt, 60))
	next, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
	require.NoError(t, err)
	assert.Nil(t, next, "message should be hidden for 60s")
	require.NoError(t, s.rsmq.DeleteMessageWithReceipt(qname, id, msg.Receipt), "receipt still valid after a visibility change")

	assert.Equal(t, ErrMessageNotFound, s.rsmq.ChangeMessageVisibilityWithReceipt(qname, "0000000000abcdefghijklmnopqrst13", "1", 0))
	assert.Equal(t, ErrReceiptMismatch, s.rsmq.ChangeMessageVisibilityWithReceipt(qname, id, "", 0))
}

func (s *RSMQSuite) TestReceiptClearedByDeletePathsAndResend() {
	t := s.T()
	qname := "que"
	require.NoError(t, s.rsmq.CreateQueue(qname, UnsetVt, UnsetDelay, UnsetMaxsize))

	id := receiptTestID
	recv := func() {
		_, err := s.rsmq.SendMessage(qname, "m", 0, WithMessageID(id))
		require.NoError(t, err)
		msg, err := s.rsmq.ReceiveMessage(qname, UnsetVt)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.NotNil(t, s.hashField(qname, id+":rt"))
	}

	recv()
	require.NoError(t, s.rsmq.DeleteMessage(qname, id))
	assert.Nil(t, s.hashField(qname, id+":rt"))

	recv()
	_, err := s.rsmq.SendMessage(qname, "m2", 0, WithMessageID(id))
	require.NoError(t, err)
	assert.Nil(t, s.hashField(qname, id+":rt"))

	require.NoError(t, s.rsmq.DeleteMessage(qname, id))
	plain, err := s.rsmq.SendMessage(qname, "p", 0)
	require.NoError(t, err)
	msg, err := s.rsmq.ReceiveMessage(qname, 0)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.NotNil(t, s.hashField(qname, plain+":rt"))
	popped, err := s.rsmq.PopMessage(qname)
	require.NoError(t, err)
	require.NotNil(t, popped)
	assert.Empty(t, popped.Receipt)
	assert.Nil(t, s.hashField(qname, plain+":rt"))
}
