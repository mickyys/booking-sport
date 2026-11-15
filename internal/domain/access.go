package domain

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Estados de una solicitud de acceso a un centro privado.
const (
	CenterAccessStatusPending  = "pending"
	CenterAccessStatusApproved = "approved"
	CenterAccessStatusRejected = "rejected"
	CenterAccessStatusRevoked  = "revoked"
)

// Origen de una solicitud.
const (
	CenterAccessSourceForm  = "form"
	CenterAccessSourceAdmin = "admin"
)

// Tipos de campo admitidos en el formulario de acceso.
const (
	CenterAccessFieldText     = "text"
	CenterAccessFieldTextarea = "textarea"
	CenterAccessFieldEmail    = "email"
	CenterAccessFieldSelect   = "select"
	CenterAccessFieldNumber   = "number"
)

// Claves reservadas de los campos de sistema.
const (
	CenterAccessFieldRUTKey   = "rut"
	CenterAccessFieldEmailKey = "email"
)

// CenterAccessField es un campo configurable del formulario de acceso.
type CenterAccessField struct {
	Key         string   `bson:"key" json:"key"`
	Label       string   `bson:"label" json:"label"`
	Type        string   `bson:"type" json:"type"`
	Required    bool     `bson:"required" json:"required"`
	Locked      bool     `bson:"locked,omitempty" json:"locked,omitempty"`
	Options     []string `bson:"options,omitempty" json:"options,omitempty"`
	Placeholder string   `bson:"placeholder,omitempty" json:"placeholder,omitempty"`
	Help        string   `bson:"help,omitempty" json:"help,omitempty"`
}

// CenterAccessForm configura la solicitud de acceso de un centro privado.
type CenterAccessForm struct {
	Enabled     bool                `bson:"enabled" json:"enabled"`
	Title       string              `bson:"title,omitempty" json:"title,omitempty"`
	Description string              `bson:"description,omitempty" json:"description,omitempty"`
	Fields      []CenterAccessField `bson:"fields,omitempty" json:"fields,omitempty"`
	UpdatedAt   time.Time           `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
	UpdatedBy   string              `bson:"updated_by,omitempty" json:"-"`
}

// DefaultCenterAccessForm devuelve el formulario con los dos campos de
// sistema. El administrador puede agregar más, pero no quitar estos.
func DefaultCenterAccessForm() *CenterAccessForm {
	return &CenterAccessForm{
		Title:       "Solicitud de acceso",
		Description: "Completa tus datos para solicitar acceso al centro.",
		Fields: []CenterAccessField{
			{Key: CenterAccessFieldRUTKey, Label: "RUT", Type: CenterAccessFieldText, Required: true, Locked: true, Placeholder: "12.345.678-9"},
			{Key: CenterAccessFieldEmailKey, Label: "Correo electrónico", Type: CenterAccessFieldEmail, Required: true, Locked: true, Placeholder: "tu@correo.cl"},
		},
	}
}

// Normalize garantiza que los campos de sistema existan, estén primero y sean
// obligatorios, sin importar lo que haya guardado el cliente.
func (f *CenterAccessForm) Normalize() {
	if f == nil {
		return
	}
	system := DefaultCenterAccessForm().Fields
	custom := make([]CenterAccessField, 0, len(f.Fields))
	for _, field := range f.Fields {
		key := strings.TrimSpace(field.Key)
		if key == "" || key == CenterAccessFieldRUTKey || key == CenterAccessFieldEmailKey {
			continue
		}
		custom = append(custom, field)
	}
	f.Fields = append(system, custom...)
}

// CenterAccessAnswer es una respuesta con la etiqueta vigente al momento de
// enviar la solicitud. Así el panel no pierde contexto si el formulario cambia.
type CenterAccessAnswer struct {
	Key   string `bson:"key" json:"key"`
	Label string `bson:"label" json:"label"`
	Value string `bson:"value" json:"value"`
}

// CenterAccessRequest es una solicitud de acceso de una persona a un centro.
type CenterAccessRequest struct {
	ID             primitive.ObjectID   `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID  primitive.ObjectID   `bson:"sport_center_id" json:"sport_center_id"`
	RUT            string               `bson:"rut" json:"rut"`
	Email          string               `bson:"email" json:"email"`
	Name           string               `bson:"name,omitempty" json:"name,omitempty"`
	Answers        []CenterAccessAnswer `bson:"answers,omitempty" json:"answers,omitempty"`
	Status         string               `bson:"status" json:"status"`
	Reason         string               `bson:"reason,omitempty" json:"reason,omitempty"`
	Source         string               `bson:"source" json:"source"`
	DecidedAt      *time.Time           `bson:"decided_at,omitempty" json:"decided_at,omitempty"`
	DecidedBy      string               `bson:"decided_by,omitempty" json:"decided_by,omitempty"`
	DecidedByEmail string               `bson:"decided_by_email,omitempty" json:"decided_by_email,omitempty"`
	VerifiedAt     *time.Time           `bson:"verified_at,omitempty" json:"verified_at,omitempty"`
	LastVerifiedAt *time.Time           `bson:"last_verified_at,omitempty" json:"last_verified_at,omitempty"`
	CreatedAt      time.Time            `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time            `bson:"updated_at" json:"updated_at"`
	CenterName     string               `bson:"-" json:"center_name,omitempty"`
}

// CenterAccessRequestFilter filtra la bandeja del administrador.
type CenterAccessRequestFilter struct {
	CenterID primitive.ObjectID
	Status   string
	Query    string
	Page     int
	Limit    int
}

// CenterAccessVerification es un código de un solo uso enviado al correo de
// una solicitud aprobada.
type CenterAccessVerification struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	RequestID     primitive.ObjectID `bson:"request_id" json:"request_id"`
	CodeHash      string             `bson:"code_hash" json:"-"`
	ExpiresAt     time.Time          `bson:"expires_at" json:"expires_at"`
	Attempts      int                `bson:"attempts" json:"attempts"`
	ConsumedAt    *time.Time         `bson:"consumed_at,omitempty" json:"consumed_at,omitempty"`
	RequestIP     string             `bson:"request_ip,omitempty" json:"-"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
}

// CenterAccessToken acredita que una persona controla el correo de una
// solicitud aprobada. Se guarda hasheado y dura 30 días.
type CenterAccessToken struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	RequestID     primitive.ObjectID `bson:"request_id" json:"request_id"`
	TokenHash     string             `bson:"token_hash" json:"-"`
	ExpiresAt     time.Time          `bson:"expires_at" json:"expires_at"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
}
