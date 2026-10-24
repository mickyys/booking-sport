package mongo

// Tests de SportCenterRepository.FindPaged con mock de MongoDB (mtest):
//	  con radio y con resultados        -> usedRadiusFallback=false
//	  con radio y sin resultados        -> fallback: reintenta sin radio y
//	                                        devuelve la cantidad por defecto
//	  sin radio                         -> sin fallback, un solo count
//	  radiusKm ausente                  -> no agrega maxDistance (implícito en
//	                                        los anteriores) y no falla

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func findPagedDoc(name, city string) bson.D {
	return bson.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "name", Value: name},
		{Key: "city", Value: city},
	}
}

func TestFindPaged_ConRadioYResultados_SinFallback(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("con_resultados", func(mt *mtest.T) {
		ns := mt.Coll.Database().Name() + "." + mt.Coll.Name()
		mt.AddMockResponses(
			// 1) count con maxDistance: hay 5 centros
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, bson.D{{Key: "total", Value: int64(5)}}),
			// 2) datos paginados
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch,
				findPagedDoc("Club Cerca", "Santiago"),
				findPagedDoc("Club Cerca 2", "Santiago"),
			),
		)

		repo := NewSportCenterRepository(mt.Coll.Database())
		lat, lng, radius := -33.45, -70.66, 20.0

		centers, total, fallback, err := repo.FindPaged(context.Background(), 1, 10, "", "", nil, nil, &lat, &lng, &radius, "")
		if err != nil {
			t.Fatalf("FindPaged: %v", err)
		}
		if fallback {
			t.Error("usedRadiusFallback = true, se esperaba false")
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if len(centers) != 2 {
			t.Errorf("len(centers) = %d, want 2", len(centers))
		}
	})
}

func TestFindPaged_RadioSinResultados_FallbackSinRadio(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("fallback", func(mt *mtest.T) {
		ns := mt.Coll.Database().Name() + "." + mt.Coll.Name()
		mt.AddMockResponses(
			// 1) count con maxDistance: sin resultados (respuesta vacía)
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch),
			// 2) recount sin maxDistance: 42 centros
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, bson.D{{Key: "total", Value: int64(42)}}),
			// 3) datos sin corte de radio
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch,
				findPagedDoc("Club Lejano", "Valparaíso"),
			),
		)

		repo := NewSportCenterRepository(mt.Coll.Database())
		lat, lng, radius := -33.45, -70.66, 1.0

		centers, total, fallback, err := repo.FindPaged(context.Background(), 1, 10, "", "", nil, nil, &lat, &lng, &radius, "")
		if err != nil {
			t.Fatalf("FindPaged: %v", err)
		}
		if !fallback {
			t.Error("usedRadiusFallback = false, se esperaba true")
		}
		if total != 42 {
			t.Errorf("total = %d, want 42 (los más cercanos sin corte de radio)", total)
		}
		if len(centers) != 1 {
			t.Errorf("len(centers) = %d, want 1", len(centers))
		}
	})
}

func TestFindPaged_SinRadio_SinFallback(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("sin_radio", func(mt *mtest.T) {
		ns := mt.Coll.Database().Name() + "." + mt.Coll.Name()
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, bson.D{{Key: "total", Value: int32(3)}}),
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, findPagedDoc("Club", "Santiago")),
		)

		repo := NewSportCenterRepository(mt.Coll.Database())
		lat, lng := -33.45, -70.66

		centers, total, fallback, err := repo.FindPaged(context.Background(), 1, 10, "", "", nil, nil, &lat, &lng, nil, "")
		if err != nil {
			t.Fatalf("FindPaged: %v", err)
		}
		if fallback {
			t.Error("usedRadiusFallback = true, se esperaba false sin radio")
		}
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
		if len(centers) != 1 {
			t.Errorf("len(centers) = %d, want 1", len(centers))
		}
	})
}

func TestFindPaged_SinCoordenadas_NoGeoNear(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("sin_coords", func(mt *mtest.T) {
		ns := mt.Coll.Database().Name() + "." + mt.Coll.Name()
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, bson.D{{Key: "total", Value: int64(1)}}),
			mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, findPagedDoc("Club", "Santiago")),
		)

		repo := NewSportCenterRepository(mt.Coll.Database())

		centers, total, fallback, err := repo.FindPaged(context.Background(), 1, 10, "", "Santiago", nil, nil, nil, nil, nil, "Santiago")
		if err != nil {
			t.Fatalf("FindPaged: %v", err)
		}
		if fallback {
			t.Error("usedRadiusFallback = true, se esperaba false")
		}
		if total != 1 {
			t.Errorf("total = %d, want 1", total)
		}
		if len(centers) != 1 {
			t.Errorf("len(centers) = %d, want 1", len(centers))
		}
	})
}
