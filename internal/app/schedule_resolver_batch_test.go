package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Los dobles embeben la interfaz completa para no tener que implementar todos
// los métodos: solo se sobrescribe el que usa LoadCenterScheduleData.

type fakeRulesByCenter struct {
	ScheduleRuleRepository
	rules []domain.ScheduleRule
	calls int
}

func (f *fakeRulesByCenter) FindBySportCenterID(context.Context, primitive.ObjectID) ([]domain.ScheduleRule, error) {
	f.calls++
	return f.rules, nil
}

type fakeExceptionsByCenter struct {
	ScheduleExceptionRepository
	exceptions []domain.ScheduleException
	calls      int
}

func (f *fakeExceptionsByCenter) FindBySportCenterID(context.Context, primitive.ObjectID) ([]domain.ScheduleException, error) {
	f.calls++
	return f.exceptions, nil
}

type fakeBlocksByCenter struct {
	BlockRepository
	blocks []domain.Block
	calls  int
}

func (f *fakeBlocksByCenter) FindBySportCenterID(context.Context, primitive.ObjectID) ([]domain.Block, error) {
	f.calls++
	return f.blocks, nil
}

func TestLoadCenterScheduleDataBatchesQueries(t *testing.T) {
	centerID := primitive.NewObjectID()
	spaceA := primitive.NewObjectID()
	spaceB := primitive.NewObjectID()
	spaceC := primitive.NewObjectID()

	loc := domain.GetSantiagoLocation()
	date := time.Date(2026, 11, 3, 15, 0, 0, 0, loc)
	weekday := int(date.Weekday())

	rules := []domain.ScheduleRule{
		{SpaceID: spaceA, DaysOfWeek: []int{0, 1, 2, 3, 4, 5, 6}, Slots: []domain.ScheduleRuleSlot{slot("09:00", "13:00", 10000)}},
		{SpaceID: spaceB, DaysOfWeek: []int{weekday}, Slots: []domain.ScheduleRuleSlot{slot("18:00", "23:00", 20000)}},
	}
	exceptions := []domain.ScheduleException{
		// Cerrado para B en la fecha consultada.
		{SpaceID: spaceB, Date: date, Type: domain.ScheduleExceptionClosed},
		// Excepción de otro día: no debe aplicarse.
		{SpaceID: spaceA, Date: date.AddDate(0, 0, 1), Type: domain.ScheduleExceptionClosed},
	}
	blocks := []domain.Block{
		{SpaceID: spaceA, Date: date, StartTime: "10:00", EndTime: "11:00"},
		// Bloqueo de otro día: no debe aplicarse.
		{SpaceID: spaceA, Date: date.AddDate(0, 0, 1), StartTime: "09:00", EndTime: "10:00"},
	}

	ruleRepo := &fakeRulesByCenter{rules: rules}
	exRepo := &fakeExceptionsByCenter{exceptions: exceptions}
	blockRepo := &fakeBlocksByCenter{blocks: blocks}
	resolver := NewScheduleResolver(ruleRepo, exRepo, blockRepo)

	data, err := resolver.LoadCenterScheduleData(context.Background(), centerID, date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Una consulta por colección, sin importar cuántos espacios tenga el centro.
	if ruleRepo.calls != 1 || exRepo.calls != 1 || blockRepo.calls != 1 {
		t.Fatalf("expected 1 call per repo, got rules=%d exceptions=%d blocks=%d",
			ruleRepo.calls, exRepo.calls, blockRepo.calls)
	}

	a := data.Resolve(spaceA)
	if a == nil || !a.HasRules {
		t.Fatalf("space A should have rules, got %+v", a)
	}
	if len(a.Franjas) != 1 || len(a.Blocks) != 1 {
		t.Errorf("space A: expected 1 franja and 1 block, got %d and %d", len(a.Franjas), len(a.Blocks))
	}
	if a.Exception != nil {
		t.Errorf("space A: exception for another date must be ignored")
	}

	b := data.Resolve(spaceB)
	if b == nil || !b.HasRules {
		t.Fatalf("space B should have rules, got %+v", b)
	}
	if b.Exception == nil || b.Exception.Type != domain.ScheduleExceptionClosed {
		t.Errorf("space B: expected closed exception, got %+v", b.Exception)
	}
	if b.Franjas != nil {
		t.Errorf("space B: closed day must have no franjas, got %v", b.Franjas)
	}

	c := data.Resolve(spaceC)
	if c == nil || c.HasRules {
		t.Errorf("space C without rules must report HasRules=false, got %+v", c)
	}
}
