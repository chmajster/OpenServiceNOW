UPDATE outbox_events SET status='DELIVERED',delivered_at=COALESCE(delivered_at,UTC_TIMESTAMP(6)),lease_owner=NULL,lease_expires_at=NULL,last_error=NULL WHERE status='FAILED';
