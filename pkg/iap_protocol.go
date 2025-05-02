package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

type IAPTunnelProtocol struct{}

// TunnelProtocol defines the protocol-specific operations.
type TunnelProtocol interface {
	ParseFrame(msg []byte) (uint16, []byte, error)
	SendDataFrame(conn *websocket.Conn, data []byte) (int, error)
	SendAckFrame(conn *websocket.Conn, length uint64) (int, error)
	SendFrame(conn *websocket.Conn, data []byte) (int, error)
	ExtractSubprotocolTag(data []byte) (uint16, []byte, error)
	ExtractSubprotocolConnectSuccessSid(data []byte) (uint64, []byte, error)
	ExtractSubprotocolAck(data []byte) (uint64, []byte, error)
	ExtractSubprotocolData(data []byte) ([]byte, []byte, error)
	CreateDataFrame(data []byte) ([]byte, error)
	CreateAckFrame(length uint64) []byte
	ValidateMessage(msg []byte) error
}

func NewIAPTunnelProtocol() TunnelProtocol {
	return &IAPTunnelProtocol{}
}

// SendDataFrame sends a data frame over the websocket connection using the IAP tunnel protocol.
func (p *IAPTunnelProtocol) SendDataFrame(conn *websocket.Conn, data []byte) (int, error) {
	frame, err := p.CreateDataFrame(data)
	if err != nil {
		return 0, fmt.Errorf("failed to create data frame: %w", err)
	}
	sent, err := p.SendFrame(conn, frame)
	if err != nil {
		return sent - SUBPROTOCOL_HEADER_LEN, fmt.Errorf("failed to send data frame: %w", err)
	}
	logger.Debug("Sent data frame", "bytes", sent, "payload_len", len(data))
	return sent - SUBPROTOCOL_HEADER_LEN, nil
}

// SendAckFrame sends an ACK frame over the websocket connection using the IAP tunnel protocol.
func (p *IAPTunnelProtocol) SendAckFrame(conn *websocket.Conn, length uint64) (int, error) {
	frame := p.CreateAckFrame(length)
	sent, err := p.SendFrame(conn, frame)
	if err != nil {
		return sent - SUBPROTOCOL_HEADER_LEN, fmt.Errorf("failed to send ACK frame: %w", err)
	}
	logger.Debug("Sent ACK frame", "bytes", sent, "ack_length", length)
	return sent - SUBPROTOCOL_HEADER_LEN, nil
}

// SendFrame sends a frame frame over the websocket connection using the IAP tunnel protocol.
func (p *IAPTunnelProtocol) SendFrame(conn *websocket.Conn, data []byte) (int, error) {
	ctx := context.Background()
	// Use websocket.MessageBinary for binary frames
	writer, err := conn.Writer(ctx, websocket.MessageBinary)
	if err != nil {
		return 0, fmt.Errorf("failed to get websocket writer: %w", err)
	}
	written, err := writer.Write(data)
	logger.Debug("Sent frame", "bytes", written, "payload_len", len(data))
	if err != nil {
		return written, writer.Close()
	}
	if err := writer.Close(); err != nil {
		return written, fmt.Errorf("failed to close websocket writer: %w", err)
	}
	return written, nil
}

func (p *IAPTunnelProtocol) ParseFrame(msg []byte) (uint16, []byte, error) {
	if err := p.ValidateMessage(msg); err != nil {
		return 0, nil, err
	}
	return p.ExtractSubprotocolTag(msg)
}

func (p *IAPTunnelProtocol) ValidateMessage(msg []byte) error {
	if len(msg) < SUBPROTOCOL_TAG_LEN {
		return errors.New("inbound message too short for subprotocol tag")
	}
	return nil
}

// ExtractSubprotocolTag extracts a 16-bit tag.
func (p *IAPTunnelProtocol) ExtractSubprotocolTag(data []byte) (uint16, []byte, error) {
	if len(data) < SUBPROTOCOL_TAG_LEN {
		return 0, nil, errors.New("incomplete data for tag")
	}
	tag := binary.BigEndian.Uint16(data[:SUBPROTOCOL_TAG_LEN])
	return tag, data[SUBPROTOCOL_TAG_LEN:], nil
}

// ExtractSubprotocolConnectSuccessSid extracts a 64-bit SID.
func (p *IAPTunnelProtocol) ExtractSubprotocolConnectSuccessSid(
	data []byte,
) (uint64, []byte, error) {
	if len(data) < 8 {
		return 0, nil, errors.New("incomplete data for connect success SID")
	}
	val := binary.BigEndian.Uint64(data[:8])
	return val, data[8:], nil
}

// ExtractSubprotocolAck extracts a 64-bit Ack.
func (p *IAPTunnelProtocol) ExtractSubprotocolAck(data []byte) (uint64, []byte, error) {
	if len(data) < 8 {
		return 0, nil, errors.New("incomplete data for ack")
	}
	val := binary.BigEndian.Uint64(data[:8])
	return val, data[8:], nil
}

// ExtractSubprotocolData extracts a length and then data of that length.
func (p *IAPTunnelProtocol) ExtractSubprotocolData(data []byte) ([]byte, []byte, error) {
	msgLen, remainder, err := extractUint32(data)
	if err != nil {
		return nil, nil, err
	}
	if len(remainder) < int(msgLen) {
		return nil, nil, errors.New("incomplete data for subprotocol payload")
	}
	payload := remainder[:msgLen]
	return payload, remainder[msgLen:], nil
}

func (p *IAPTunnelProtocol) CreateDataFrame(data []byte) ([]byte, error) {
	if len(data) > int(^uint32(0)) { // or use math.MaxUint32
		return nil, fmt.Errorf("payload too large for protocol frame: %d bytes", len(data))
	}
	frame := make([]byte, SUBPROTOCOL_HEADER_LEN+len(data))
	binary.BigEndian.PutUint16(frame[0:], SUBPROTOCOL_TAG_DATA)
	binary.BigEndian.PutUint32(frame[SUBPROTOCOL_TAG_LEN:], uint32(len(data))) //#nosec G115
	copy(frame[SUBPROTOCOL_TAG_LEN+4:], data)
	return frame, nil
}

func (p *IAPTunnelProtocol) CreateAckFrame(length uint64) []byte {
	frame := make([]byte, SUBPROTOCOL_TAG_LEN+8)
	binary.BigEndian.PutUint16(frame[0:], SUBPROTOCOL_TAG_ACK)
	binary.BigEndian.PutUint64(frame[SUBPROTOCOL_TAG_LEN:], length)
	return frame
}
