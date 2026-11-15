package mongo

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// El buscador del panel superadmin debe encontrar reservas creadas desde el
// panel o por series recurrentes, que guardan al cliente solo en
// customer_name/customer_phone (sin guest_details ni user_id).
func TestBookingFilterSearchesCustomerNameAndPhone(t *testing.T) {
	f := bookingFilter(SuperAdminBookingFilter{Query: "ricardo"})

	or, ok := f["$or"].(bson.A)
	if !ok {
		t.Fatalf("$or type = %T, want bson.A", f["$or"])
	}

	got := map[string]bool{}
	for _, cond := range or {
		doc, ok := cond.(bson.M)
		if !ok {
			continue
		}
		for key := range doc {
			got[key] = true
		}
	}

	for _, want := range []string{"booking_code", "guest_details.email", "guest_details.name", "user_id", "customer_name", "customer_phone"} {
		if !got[want] {
			t.Errorf("falta %q en el $or del filtro de búsqueda", want)
		}
	}
}
