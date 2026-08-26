package inbox

// messageListSelectSQL is the shared SELECT/FROM for inbox message lists.
// Uses LEFT JOIN on order.payment_proof_message_id (qualified via OpenTenantScope).
const messageListSelectSQL = `SELECT m.id, m.conversation_id, m.external_id, m.direction, m.author, m.type, m.body, m.status, m.created_at, m.metadata,
        o.id::text
 FROM message m
 LEFT JOIN "order" o ON o.payment_proof_message_id = m.id AND o.deleted_at IS NULL`
