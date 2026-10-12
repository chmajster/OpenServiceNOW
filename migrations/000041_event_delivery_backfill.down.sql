DELETE d FROM event_deliveries d JOIN outbox_events e ON e.id=d.event_id WHERE e.notification_eligible=FALSE;
