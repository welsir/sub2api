// [INPUT]: Ent client and activation journey service contract.
// [OUTPUT]: Activation journey repository constructor.
// [POS]: Persistence adapter for auditable HVOY activation journeys.
//
// [PROTOCOL]:
// 1. Update this header when journey persistence behavior changes.
// 2. Keep activation policy, benefit grants, and notifications out of this adapter.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	dbjourney "github.com/Wei-Shaw/sub2api/ent/useractivationjourney"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type userActivationJourneyRepository struct {
	client *dbent.Client
}

func NewUserActivationJourneyRepository(client *dbent.Client) service.UserActivationJourneyRepository {
	return &userActivationJourneyRepository{client: client}
}

func (r *userActivationJourneyRepository) CreateIfAbsent(
	ctx context.Context,
	userID int64,
	source string,
) (*service.UserActivationJourney, bool, error) {
	client := clientFromContext(ctx, r.client)
	id, err := client.UserActivationJourney.Create().
		SetUserID(userID).
		SetCampaignSource(source).
		OnConflictColumns(dbjourney.FieldUserID).
		DoNothing().
		ID(ctx)
	if err == nil {
		entity, err := client.UserActivationJourney.Query().
			Where(
				dbjourney.IDEQ(id),
				dbjourney.UserIDEQ(userID),
			).
			Only(ctx)
		if err != nil {
			return nil, false, err
		}
		return activationJourneyEntityToService(entity), true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	entity, err := client.UserActivationJourney.Query().
		Where(dbjourney.UserIDEQ(userID)).
		Only(ctx)
	if err != nil {
		return nil, false, translatePersistenceError(err, service.ErrUserActivationJourneyNotFound, nil)
	}
	return activationJourneyEntityToService(entity), false, nil
}

func (r *userActivationJourneyRepository) GetByUserID(
	ctx context.Context,
	userID int64,
) (*service.UserActivationJourney, error) {
	client := clientFromContext(ctx, r.client)
	entity, err := client.UserActivationJourney.Query().
		Where(dbjourney.UserIDEQ(userID)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserActivationJourneyNotFound, nil)
	}
	return activationJourneyEntityToService(entity), nil
}

func (r *userActivationJourneyRepository) GetByUserIDForUpdate(
	ctx context.Context,
	userID int64,
) (*service.UserActivationJourney, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil, service.ErrUserActivationJourneyTransactionRequired
	}
	client := tx.Client()
	entity, err := client.UserActivationJourney.Query().
		Where(dbjourney.UserIDEQ(userID)).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserActivationJourneyNotFound, nil)
	}
	return activationJourneyEntityToService(entity), nil
}

func (r *userActivationJourneyRepository) Update(
	ctx context.Context,
	journey *service.UserActivationJourney,
) error {
	if journey == nil {
		return errors.New("activation journey is nil")
	}

	client := clientFromContext(ctx, r.client)
	update := client.UserActivationJourney.UpdateOneID(journey.ID).
		SetStarterState(dbjourney.StarterState(journey.StarterState)).
		SetRecallState(dbjourney.RecallState(journey.RecallState))

	if journey.StarterSubscriptionID == nil {
		update.ClearStarterSubscriptionID()
	} else {
		update.SetStarterSubscriptionID(*journey.StarterSubscriptionID)
	}
	if journey.StarterGrantedAt == nil {
		update.ClearStarterGrantedAt()
	} else {
		update.SetStarterGrantedAt(*journey.StarterGrantedAt)
	}
	if journey.StarterExpiresAt == nil {
		update.ClearStarterExpiresAt()
	} else {
		update.SetStarterExpiresAt(*journey.StarterExpiresAt)
	}
	if journey.RecallSubscriptionID == nil {
		update.ClearRecallSubscriptionID()
	} else {
		update.SetRecallSubscriptionID(*journey.RecallSubscriptionID)
	}
	if journey.RecallClaimedAt == nil {
		update.ClearRecallClaimedAt()
	} else {
		update.SetRecallClaimedAt(*journey.RecallClaimedAt)
	}
	if journey.RecallExpiresAt == nil {
		update.ClearRecallExpiresAt()
	} else {
		update.SetRecallExpiresAt(*journey.RecallExpiresAt)
	}
	if journey.FirstSuccessAt == nil {
		update.ClearFirstSuccessAt()
	} else {
		update.SetFirstSuccessAt(*journey.FirstSuccessAt)
	}
	if journey.NoAttemptEmailSentAt == nil {
		update.ClearNoAttemptEmailSentAt()
	} else {
		update.SetNoAttemptEmailSentAt(*journey.NoAttemptEmailSentAt)
	}
	if journey.AttemptedEmailSentAt == nil {
		update.ClearAttemptedEmailSentAt()
	} else {
		update.SetAttemptedEmailSentAt(*journey.AttemptedEmailSentAt)
	}
	if journey.PaidSupportEmailSentAt == nil {
		update.ClearPaidSupportEmailSentAt()
	} else {
		update.SetPaidSupportEmailSentAt(*journey.PaidSupportEmailSentAt)
	}
	if journey.RecallAvailableEmailSentAt == nil {
		update.ClearRecallAvailableEmailSentAt()
	} else {
		update.SetRecallAvailableEmailSentAt(*journey.RecallAvailableEmailSentAt)
	}
	if journey.RecallExpiredEmailSentAt == nil {
		update.ClearRecallExpiredEmailSentAt()
	} else {
		update.SetRecallExpiredEmailSentAt(*journey.RecallExpiredEmailSentAt)
	}
	if journey.LastEmailSentAt == nil {
		update.ClearLastEmailSentAt()
	} else {
		update.SetLastEmailSentAt(*journey.LastEmailSentAt)
	}
	if journey.LastEvaluatedAt == nil {
		update.ClearLastEvaluatedAt()
	} else {
		update.SetLastEvaluatedAt(*journey.LastEvaluatedAt)
	}

	entity, err := update.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrUserActivationJourneyNotFound, nil)
	}
	*journey = *activationJourneyEntityToService(entity)
	return nil
}

func (r *userActivationJourneyRepository) ListDue(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]service.UserActivationJourney, error) {
	if limit <= 0 {
		return []service.UserActivationJourney{}, nil
	}

	client := clientFromContext(ctx, r.client)
	entities, err := client.UserActivationJourney.Query().
		Where(dbjourney.Or(
			dbjourney.LastEvaluatedAtIsNil(),
			dbjourney.LastEvaluatedAtLTE(now),
		)).
		Order(
			dbjourney.ByLastEvaluatedAt(entsql.OrderNullsFirst()),
			dbjourney.ByID(),
		).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return activationJourneyEntitiesToService(entities), nil
}

func (r *userActivationJourneyRepository) ListEligibleUsersWithoutJourney(
	ctx context.Context,
	eligibleAfter time.Time,
	limit int,
) ([]service.User, error) {
	if limit <= 0 {
		return []service.User{}, nil
	}

	client := clientFromContext(ctx, r.client)
	entities, err := client.User.Query().
		Where(
			dbuser.CreatedAtGTE(eligibleAfter),
			dbuser.SignupSourceEQ("email"),
			dbuser.DeletedAtIsNil(),
			dbuser.Not(dbuser.HasActivationJourney()),
		).
		Order(dbuser.ByCreatedAt(), dbuser.ByID()).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]service.User, 0, len(entities))
	for _, entity := range entities {
		users = append(users, *userEntityToService(entity))
	}
	return users, nil
}

func activationJourneyEntityToService(entity *dbent.UserActivationJourney) *service.UserActivationJourney {
	if entity == nil {
		return nil
	}
	return &service.UserActivationJourney{
		ID:                         entity.ID,
		UserID:                     entity.UserID,
		CampaignSource:             entity.CampaignSource,
		StarterState:               string(entity.StarterState),
		StarterSubscriptionID:      entity.StarterSubscriptionID,
		StarterGrantedAt:           entity.StarterGrantedAt,
		StarterExpiresAt:           entity.StarterExpiresAt,
		RecallState:                string(entity.RecallState),
		RecallSubscriptionID:       entity.RecallSubscriptionID,
		RecallClaimedAt:            entity.RecallClaimedAt,
		RecallExpiresAt:            entity.RecallExpiresAt,
		FirstSuccessAt:             entity.FirstSuccessAt,
		NoAttemptEmailSentAt:       entity.NoAttemptEmailSentAt,
		AttemptedEmailSentAt:       entity.AttemptedEmailSentAt,
		PaidSupportEmailSentAt:     entity.PaidSupportEmailSentAt,
		RecallAvailableEmailSentAt: entity.RecallAvailableEmailSentAt,
		RecallExpiredEmailSentAt:   entity.RecallExpiredEmailSentAt,
		LastEmailSentAt:            entity.LastEmailSentAt,
		LastEvaluatedAt:            entity.LastEvaluatedAt,
		CreatedAt:                  entity.CreatedAt,
		UpdatedAt:                  entity.UpdatedAt,
	}
}

func activationJourneyEntitiesToService(entities []*dbent.UserActivationJourney) []service.UserActivationJourney {
	journeys := make([]service.UserActivationJourney, 0, len(entities))
	for _, entity := range entities {
		journeys = append(journeys, *activationJourneyEntityToService(entity))
	}
	return journeys
}
