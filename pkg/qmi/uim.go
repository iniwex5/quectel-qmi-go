package qmi

import (
	"context"
	"fmt"
)

// ============================================================================
// UIM Service wrapper / UIM服务包装器
// ============================================================================

type UIMService struct {
	client   *Client
	clientID uint8
}

// NewUIMService creates a UIM service wrapper / NewUIMService创建一个UIM服务包装器
func NewUIMService(client *Client) (*UIMService, error) {
	clientID, err := client.AllocateClientID(ServiceUIM)
	if err != nil {
		return nil, err
	}
	return &UIMService{client: client, clientID: clientID}, nil
}

// Close releases the UIM client ID / Close释放UIM客户端ID
func (u *UIMService) Close() error {
	return u.client.ReleaseClientID(ServiceUIM, u.clientID)
}

// GetCardStatus queries the UIM card status / GetCardStatus查询UIM卡状态
func (u *UIMService) GetCardStatus(ctx context.Context) (SIMStatus, error) {
	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMGetCardStatus, nil)
	if err != nil {
		return SIMAbsent, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return SIMAbsent, fmt.Errorf("UIM get card status failed: 0x%04x", qmiErr)
	}

	// TLV 0x10: Card status / TLV 0x10: 卡状态
	// Struct: IndexGWPri(2) + Index1XPri(2) + IndexGWSec(2) + Index1XSec(2) + NumSlot(1) + CardState(1)
	// Offset: 0 + 2 + 2 + 2 + 2 + 1 = 9
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 10 {
		cardState := tlv.Value[9]
		switch cardState {
		case 0x01: // PRESENT / 存在 (PRESENT)
			return SIMReady, nil
		case 0x00: // ABSENT / 不在 (ABSENT)
			return SIMAbsent, nil
		case 0x02: // ERROR / 错误 (ERROR)
			return SIMBlocked, nil
		}
	}

	return SIMNotReady, nil
}
