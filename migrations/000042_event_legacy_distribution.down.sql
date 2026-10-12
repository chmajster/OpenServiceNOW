UPDATE outbox_events e JOIN event_deliveries d ON d.event_id=e.id AND d.consumer_id='activity.v1' SET e.status='FAILED' WHERE e.notification_eligible=FALSE AND d.status='FAILED';
