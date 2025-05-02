package tunnel

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDataFrameAndParseFrame(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	payload := []byte("hello world")
	frame, err := protocol.CreateDataFrame(payload)
	require.NoError(t, err)
	assert.NotEmpty(t, frame, "frame should not be empty")
	assert.Equal(t, len(frame), len(payload)+SUBPROTOCOL_HEADER_LEN, "frame length mismatch")

	// Frame should start with SUBPROTOCOL_TAG_DATA
	tag, rest, err := protocol.ExtractSubprotocolTag(frame)
	require.NoError(t, err)
	assert.Equal(t, SUBPROTOCOL_TAG_DATA, tag)

	// Extract data
	data, _, err := protocol.ExtractSubprotocolData(rest)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, payload), "expected payload %q, got %q", payload, data)
}

func TestCreateAckFrameAndExtractAck(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	ackLen := uint64(12345)
	frame := protocol.CreateAckFrame(ackLen)

	tag, rest, err := protocol.ExtractSubprotocolTag(frame)
	require.NoError(t, err)
	assert.Equal(t, SUBPROTOCOL_TAG_ACK, tag)

	ack, _, err := protocol.ExtractSubprotocolAck(rest)
	require.NoError(t, err)
	assert.Equal(t, ackLen, ack)
}

func TestExtractSubprotocolConnectSuccessSid(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	sid := uint64(0xdeadbeefcafebabe)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, sid)

	gotSid, rest, err := protocol.ExtractSubprotocolConnectSuccessSid(buf)
	require.NoError(t, err)
	assert.Equal(t, sid, gotSid)
	assert.Empty(t, rest)
}

func TestValidateMessage(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	short := []byte{0x01}
	err := protocol.ValidateMessage(short)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inbound message too short for subprotocol tag")

	ok := make([]byte, SUBPROTOCOL_TAG_LEN)
	err = protocol.ValidateMessage(ok)
	assert.NoError(t, err)
}

func TestExtractSubprotocolTagErrors(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	_, _, err := protocol.ExtractSubprotocolTag([]byte{0x01})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete data for tag")
}

func TestExtractSubprotocolAckErrors(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	_, _, err := protocol.ExtractSubprotocolAck([]byte{0x01, 0x02})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete data for ack")
}

func TestExtractDataErrors(t *testing.T) {
	protocol := NewIAPTunnelProtocol()
	// Not enough bytes for length
	_, _, err := protocol.ExtractSubprotocolData([]byte{0x01})
	assert.Error(t, err)
	// Length longer than actual data
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, 10)
	_, _, err = protocol.ExtractSubprotocolData(buf)
	assert.Error(t, err)
}
