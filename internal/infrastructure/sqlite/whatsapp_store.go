package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

func (s *Store) ManagedWhatsAppInstances() ([]whatsapp.ManagedInstance, error) {
	rows, err := s.db.Query(`SELECT instance_name, inbox_id, inbox_name, owner_jid, connection_state,
		chatwoot_enabled, bridge_enabled, ai_enabled, last_audit_status, last_audit_detail,
		COALESCE(last_checked_at,''), COALESCE(attached_at,''), COALESCE(detached_at,''), updated_at
		FROM managed_whatsapp_instances ORDER BY instance_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []whatsapp.ManagedInstance
	for rows.Next() {
		row, err := scanManagedInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) ManagedWhatsAppInstance(instance string) (*whatsapp.ManagedInstance, error) {
	row := s.db.QueryRow(`SELECT instance_name, inbox_id, inbox_name, owner_jid, connection_state,
		chatwoot_enabled, bridge_enabled, ai_enabled, last_audit_status, last_audit_detail,
		COALESCE(last_checked_at,''), COALESCE(attached_at,''), COALESCE(detached_at,''), updated_at
		FROM managed_whatsapp_instances WHERE instance_name=?`, instance)
	out, err := scanManagedInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) UpsertManagedWhatsAppInstance(row whatsapp.ManagedInstance, actor string) error {
	if row.InstanceName == "" {
		return fmt.Errorf("instance name is required")
	}
	_, err := s.db.Exec(`INSERT INTO managed_whatsapp_instances
		(instance_name, inbox_id, inbox_name, owner_jid, connection_state, chatwoot_enabled,
		 bridge_enabled, ai_enabled, last_audit_status, last_audit_detail, last_checked_at,
		 attached_at, detached_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,datetime('now'),datetime('now'),NULL,datetime('now'))
		ON CONFLICT(instance_name) DO UPDATE SET
			inbox_id=excluded.inbox_id,
			inbox_name=excluded.inbox_name,
			owner_jid=excluded.owner_jid,
			connection_state=excluded.connection_state,
			chatwoot_enabled=excluded.chatwoot_enabled,
			bridge_enabled=excluded.bridge_enabled,
			ai_enabled=excluded.ai_enabled,
			last_audit_status=excluded.last_audit_status,
			last_audit_detail=excluded.last_audit_detail,
			last_checked_at=datetime('now'),
			attached_at=COALESCE(managed_whatsapp_instances.attached_at, datetime('now')),
			detached_at=NULL,
			updated_at=datetime('now')`,
		row.InstanceName, row.InboxID, row.InboxName, row.OwnerJID, row.ConnectionState,
		boolToInt(row.ChatwootEnabled), boolToInt(row.BridgeEnabled), boolToInt(row.AIEnabled),
		row.LastAuditStatus, row.LastAuditDetail)
	if err != nil {
		return err
	}
	s.audit(actor, "whatsapp_attach", row.InstanceName)
	return nil
}

func (s *Store) SetManagedWhatsAppDetached(instance string, actor string) error {
	_, err := s.db.Exec(`UPDATE managed_whatsapp_instances SET
		chatwoot_enabled=0,
		bridge_enabled=0,
		ai_enabled=0,
		last_audit_status='detached',
		last_audit_detail='chatwoot bridge disabled; whatsapp session left intact',
		last_checked_at=datetime('now'),
		detached_at=datetime('now'),
		updated_at=datetime('now')
		WHERE instance_name=?`, instance)
	if err != nil {
		return err
	}
	s.audit(actor, "whatsapp_detach", instance)
	return nil
}

func (s *Store) AIEnabledInboxIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT inbox_id FROM managed_whatsapp_instances
		WHERE ai_enabled=1 AND inbox_id > 0 ORDER BY inbox_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type managedScanner interface {
	Scan(dest ...any) error
}

func scanManagedInstance(row managedScanner) (whatsapp.ManagedInstance, error) {
	var out whatsapp.ManagedInstance
	var chatwootEnabled, bridgeEnabled, aiEnabled int
	err := row.Scan(&out.InstanceName, &out.InboxID, &out.InboxName, &out.OwnerJID, &out.ConnectionState,
		&chatwootEnabled, &bridgeEnabled, &aiEnabled, &out.LastAuditStatus, &out.LastAuditDetail,
		&out.LastCheckedAt, &out.AttachedAt, &out.DetachedAt, &out.UpdatedAt)
	out.ChatwootEnabled = chatwootEnabled == 1
	out.BridgeEnabled = bridgeEnabled == 1
	out.AIEnabled = aiEnabled == 1
	return out, err
}
