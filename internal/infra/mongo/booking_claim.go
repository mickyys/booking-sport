package mongo

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// emailCollation hace la comparación de correos insensible a mayúsculas.
// Debe coincidir con la colación del índice idx_bookings_guest_email.
var emailCollation = &options.Collation{Locale: "en", Strength: 2}

// ClaimByEmail asocia al usuario las reservas hechas como invitado con ese
// correo y que aún no tienen dueño. Devuelve cuántas reservas vinculó.
func (r *BookingRepository) ClaimByEmail(ctx context.Context, email, userID string) (int64, error) {
	email = strings.TrimSpace(email)
	if email == "" || userID == "" {
		return 0, nil
	}
	filter := bson.M{
		"guest_details.email": email,
		"$or": []bson.M{
			{"user_id": bson.M{"$exists": false}},
			{"user_id": ""},
		},
	}
	res, err := r.collection.UpdateMany(ctx, filter,
		bson.M{"$set": bson.M{"user_id": userID, "updated_at": time.Now()}},
		options.Update().SetCollation(emailCollation),
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
