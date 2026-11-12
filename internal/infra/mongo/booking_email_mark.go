package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// MarkConfirmationEmailSent deja constancia en la reserva de que el correo de
// confirmación fue aceptado por el proveedor. El código puede ser el de la
// reserva o el de grupo (reservas de espacios por varios días).
func (r *BookingRepository) MarkConfirmationEmailSent(ctx context.Context, code, messageID string) error {
	now := time.Now().UTC()
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"$or": []bson.M{{"booking_code": code}, {"group_code": code}}},
		bson.M{"$set": bson.M{
			"confirmation_email_sent_at":    now,
			"confirmation_email_message_id": messageID,
		}},
	)
	return err
}
