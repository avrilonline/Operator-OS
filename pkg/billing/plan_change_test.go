package billing

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// ---------- Helpers ----------

func newTestSubStore(t *testing.T) (*SQLiteSubscriptionStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	store, err := NewSQLiteSubscriptionStore(db)
	require.NoError(t, err)
	return store, db
}

func newTestPlanChangeService(t *testing.T) (*PlanChangeService, *SQLiteSubscriptionStore) {
	t.Helper()
	store, _ := newTestSubStore(t)
	svc, err := NewPlanChangeService(PlanChangeConfig{
		SubStore:  store,
		Catalogue: NewCatalogue(nil),
	})
	require.NoError(t, err)
	return svc, store
}

func createTestSubscription(t *testing.T, store *SQLiteSubscriptionStore, userID string, planID PlanID) *Subscription {
	t.Helper()
	now := time.Now().UTC()
	sub := &Subscription{
		ID:                 "sub-" + userID,
		UserID:             userID,
		PlanID:             planID,
		Status:             SubStatusActive,
		BillingInterval:    IntervalMonthly,
		CurrentPeriodStart: now.AddDate(0, 0, -15),
		CurrentPeriodEnd:   now.AddDate(0, 0, 15),
	}
	require.NoError(t, store.Create(sub))
	return sub
}

// withUserCtx is defined in usage_api_test.go

// ---------- PlanTierOrder Tests ----------

func TestPlanTierOrder(t *testing.T) {
	assert.Equal(t, 0, PlanTierOrder(PlanFree))
	assert.Equal(t, 1, PlanTierOrder(PlanStarter))
	assert.Equal(t, 2, PlanTierOrder(PlanPro))
	assert.Equal(t, 3, PlanTierOrder(PlanEnterprise))
	assert.Equal(t, -1, PlanTierOrder("invalid"))
}

// ---------- ComparePlans Tests ----------

func TestComparePlans(t *testing.T) {
	tests := []struct {
		name string
		from PlanID
		to   PlanID
		want PlanChangeDirection
	}{
		{"free to starter", PlanFree, PlanStarter, DirectionUpgrade},
		{"free to pro", PlanFree, PlanPro, DirectionUpgrade},
		{"starter to pro", PlanStarter, PlanPro, DirectionUpgrade},
		{"pro to enterprise", PlanPro, PlanEnterprise, DirectionUpgrade},
		{"pro to starter", PlanPro, PlanStarter, DirectionDowngrade},
		{"starter to free", PlanStarter, PlanFree, DirectionDowngrade},
		{"enterprise to free", PlanEnterprise, PlanFree, DirectionDowngrade},
		{"same plan", PlanPro, PlanPro, DirectionSame},
		{"free same", PlanFree, PlanFree, DirectionSame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComparePlans(tt.from, tt.to)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------- DefaultChangeMode Tests ----------

func TestDefaultChangeMode(t *testing.T) {
	assert.Equal(t, ModeImmediate, DefaultChangeMode(DirectionUpgrade))
	assert.Equal(t, ModeAtPeriodEnd, DefaultChangeMode(DirectionDowngrade))
	assert.Equal(t, ModeImmediate, DefaultChangeMode(DirectionSame))
}

// ---------- NewPlanChangeService Tests ----------

func TestNewPlanChangeService_NilStore(t *testing.T) {
	_, err := NewPlanChangeService(PlanChangeConfig{
		SubStore:  nil,
		Catalogue: NewCatalogue(nil),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscription store is required")
}

func TestNewPlanChangeService_NilCatalogue(t *testing.T) {
	store, _ := newTestSubStore(t)
	_, err := NewPlanChangeService(PlanChangeConfig{
		SubStore:  store,
		Catalogue: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalogue is required")
}

func TestNewPlanChangeService_Success(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	require.NotNil(t, svc)
}

// ---------- ChangePlan Tests ----------

func TestChangePlan_EmptyUserID(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	_, err := svc.ChangePlan("", PlanChangeRequest{NewPlanID: PlanStarter})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestChangePlan_InvalidPlan(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	_, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: "invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid plan")
}

func TestChangePlan_PlanNotFound(t *testing.T) {
	// Create a catalogue without the enterprise plan.
	plans := map[PlanID]*Plan{
		PlanFree: {ID: PlanFree, Name: "Free", Active: true},
	}
	store, _ := newTestSubStore(t)
	svc, err := NewPlanChangeService(PlanChangeConfig{
		SubStore:  store,
		Catalogue: NewCatalogue(plans),
	})
	require.NoError(t, err)

	_, err = svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestChangePlan_InactivePlan(t *testing.T) {
	plans := DefaultPlans()
	plans[PlanStarter].Active = false
	store, _ := newTestSubStore(t)
	svc, err := NewPlanChangeService(PlanChangeConfig{
		SubStore:  store,
		Catalogue: NewCatalogue(plans),
	})
	require.NoError(t, err)

	_, err = svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestChangePlan_AlreadyOnPlan(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)

	_, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already on plan")
}

func TestChangePlan_InvalidMode(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	_, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanStarter,
		Mode:      "invalid_mode",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid change mode")
}

func TestChangePlan_UpgradeFromFree(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	assert.Equal(t, DirectionUpgrade, result.Direction)
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, PlanFree, result.PreviousPlan)
	assert.Equal(t, PlanStarter, result.NewPlan)
	assert.NotNil(t, result.Subscription)
	assert.Equal(t, PlanStarter, result.Subscription.PlanID)
	assert.Equal(t, SubStatusActive, result.Subscription.Status)
	assert.Equal(t, IntervalMonthly, result.Subscription.BillingInterval)
}

func TestChangePlan_UpgradeFromFreeYearly(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanPro,
		Interval:  IntervalYearly,
	})
	require.NoError(t, err)
	assert.Equal(t, DirectionUpgrade, result.Direction)
	assert.Equal(t, PlanPro, result.NewPlan)
	assert.Equal(t, IntervalYearly, result.Subscription.BillingInterval)
	// Yearly period should be ~365 days.
	diff := result.Subscription.CurrentPeriodEnd.Sub(result.Subscription.CurrentPeriodStart)
	assert.InDelta(t, 365*24*time.Hour, diff, float64(48*time.Hour))
}

func TestChangePlan_UpgradePaidToPaid(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanPro})
	require.NoError(t, err)
	assert.Equal(t, DirectionUpgrade, result.Direction)
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, PlanStarter, result.PreviousPlan)
	assert.Equal(t, PlanPro, result.NewPlan)
	assert.Equal(t, PlanPro, result.Subscription.PlanID)
	// Proration should be positive (upgrade = more expensive).
	assert.Greater(t, result.ProrationAmount, int64(0))
}

func TestChangePlan_DowngradePaidToPaid(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanPro)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	assert.Equal(t, DirectionDowngrade, result.Direction)
	// Default mode for downgrade is at_period_end.
	// But without Stripe, it falls back to immediate.
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, PlanPro, result.PreviousPlan)
	assert.Equal(t, PlanStarter, result.NewPlan)
	assert.Equal(t, PlanStarter, result.Subscription.PlanID)
	// Proration should be negative (downgrade = cheaper).
	assert.Less(t, result.ProrationAmount, int64(0))
}

func TestChangePlan_DowngradeToFreeImmediate(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanFree,
		Mode:      ModeImmediate,
	})
	require.NoError(t, err)
	assert.Equal(t, DirectionDowngrade, result.Direction)
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, SubStatusCanceled, result.Subscription.Status)
	assert.Equal(t, PlanFree, result.Subscription.PlanID)
}

func TestChangePlan_DowngradeToFreeAtPeriodEnd(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	sub := createTestSubscription(t, store, "user1", PlanStarter)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanFree,
		Mode:      ModeAtPeriodEnd,
	})
	require.NoError(t, err)
	assert.Equal(t, DirectionDowngrade, result.Direction)
	assert.Equal(t, ModeAtPeriodEnd, result.Mode)
	assert.True(t, result.Subscription.CancelAtPeriodEnd)
	assert.Equal(t, sub.CurrentPeriodEnd.Unix(), result.EffectiveAt.Unix())
}

func TestChangePlan_DowngradeToFreeNoSubscription(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	// User with no subscription is already on free plan.
	_, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanFree,
		Mode:      ModeImmediate,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already on plan")
}

func TestChangePlan_UpgradeImmediateMode(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanPro,
		Mode:      ModeImmediate,
	})
	require.NoError(t, err)
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, PlanPro, result.Subscription.PlanID)
}

func TestChangePlan_DowngradeWithExplicitImmediateMode(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanPro)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{
		NewPlanID: PlanStarter,
		Mode:      ModeImmediate,
	})
	require.NoError(t, err)
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, PlanStarter, result.Subscription.PlanID)
}

func TestChangePlan_OnPlanChangedCallback(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)

	var callbackUser string
	var callbackResult *PlanChangeResult
	svc.OnPlanChanged = func(userID string, result *PlanChangeResult) {
		callbackUser = userID
		callbackResult = result
	}

	_, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	assert.Equal(t, "user1", callbackUser)
	assert.NotNil(t, callbackResult)
	assert.Equal(t, PlanStarter, callbackResult.NewPlan)
}

func TestChangePlan_MultiUser(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)

	// user2 upgrades from free.
	result2, err := svc.ChangePlan("user2", PlanChangeRequest{NewPlanID: PlanPro})
	require.NoError(t, err)
	assert.Equal(t, PlanFree, result2.PreviousPlan)
	assert.Equal(t, PlanPro, result2.NewPlan)

	// user1 upgrades from starter.
	result1, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanPro})
	require.NoError(t, err)
	assert.Equal(t, PlanStarter, result1.PreviousPlan)
	assert.Equal(t, PlanPro, result1.NewPlan)

	// Verify both users are on pro.
	sub1, err := store.GetByUserID("user1")
	require.NoError(t, err)
	assert.Equal(t, PlanPro, sub1.PlanID)

	sub2, err := store.GetByUserID("user2")
	require.NoError(t, err)
	assert.Equal(t, PlanPro, sub2.PlanID)
}

// ---------- Proration Tests ----------

func TestEstimateProration_Upgrade(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	now := time.Now().UTC()
	sub := &Subscription{
		ID:                 "sub-prorate",
		UserID:             "user1",
		PlanID:             PlanStarter,
		Status:             SubStatusActive,
		BillingInterval:    IntervalMonthly,
		CurrentPeriodStart: now.AddDate(0, 0, -15),
		CurrentPeriodEnd:   now.AddDate(0, 0, 15),
	}
	require.NoError(t, store.Create(sub))

	// Proration for upgrading starter ($9) → pro ($29), ~50% remaining.
	amount := svc.estimateProration(sub, PlanPro, IntervalMonthly, now)
	assert.Greater(t, amount, int64(0))
	// Should be roughly ($29-$9) * 0.5 = $10 = 1000 cents.
	assert.InDelta(t, 1000, amount, 200) // allow tolerance for time drift
}

func TestEstimateProration_Downgrade(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	now := time.Now().UTC()
	sub := &Subscription{
		ID:                 "sub-prorate-down",
		UserID:             "user1",
		PlanID:             PlanPro,
		Status:             SubStatusActive,
		BillingInterval:    IntervalMonthly,
		CurrentPeriodStart: now.AddDate(0, 0, -15),
		CurrentPeriodEnd:   now.AddDate(0, 0, 15),
	}
	require.NoError(t, store.Create(sub))

	// Proration for downgrading pro ($29) → starter ($9), ~50% remaining.
	amount := svc.estimateProration(sub, PlanStarter, IntervalMonthly, now)
	assert.Less(t, amount, int64(0))
	assert.InDelta(t, -1000, amount, 200)
}

func TestEstimateProration_PeriodEnded(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	now := time.Now().UTC()
	sub := &Subscription{
		PlanID:             PlanStarter,
		BillingInterval:    IntervalMonthly,
		CurrentPeriodStart: now.AddDate(0, -1, -15),
		CurrentPeriodEnd:   now.AddDate(0, 0, -1), // already ended
	}

	amount := svc.estimateProration(sub, PlanPro, IntervalMonthly, now)
	assert.Equal(t, int64(0), amount)
}

func TestEstimateProration_NilPlans(t *testing.T) {
	plans := map[PlanID]*Plan{
		PlanFree: {ID: PlanFree, Name: "Free", Active: true},
	}
	store, _ := newTestSubStore(t)
	svc, err := NewPlanChangeService(PlanChangeConfig{
		SubStore:  store,
		Catalogue: NewCatalogue(plans),
	})
	require.NoError(t, err)

	sub := &Subscription{
		PlanID:          PlanStarter, // not in catalogue
		BillingInterval: IntervalMonthly,
	}
	amount := svc.estimateProration(sub, PlanPro, IntervalMonthly, time.Now())
	assert.Equal(t, int64(0), amount)
}

// ---------- Preview Tests ----------

func TestPreview_EmptyUserID(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	_, err := svc.Preview("", PlanChangeRequest{NewPlanID: PlanStarter})
	require.Error(t, err)
}

func TestPreview_InvalidPlan(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	_, err := svc.Preview("user1", PlanChangeRequest{NewPlanID: "bad"})
	require.Error(t, err)
}

func TestPreview_UpgradeFromFree(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	result, err := svc.Preview("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	assert.Equal(t, DirectionUpgrade, result.Direction)
	assert.Equal(t, ModeImmediate, result.Mode)
	assert.Equal(t, PlanFree, result.PreviousPlan)
	assert.Equal(t, PlanStarter, result.NewPlan)
	assert.Nil(t, result.Subscription) // preview doesn't create anything
}

func TestPreview_Downgrade(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanPro)

	result, err := svc.Preview("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	assert.Equal(t, DirectionDowngrade, result.Direction)
	assert.Equal(t, ModeAtPeriodEnd, result.Mode)
	assert.Less(t, result.ProrationAmount, int64(0))
}

func TestPreview_SamePlan(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)

	result, err := svc.Preview("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	assert.Equal(t, DirectionSame, result.Direction)
}

func TestPreview_WithExplicitMode(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanPro)

	result, err := svc.Preview("user1", PlanChangeRequest{
		NewPlanID: PlanStarter,
		Mode:      ModeImmediate,
	})
	require.NoError(t, err)
	assert.Equal(t, ModeImmediate, result.Mode)
}

// ---------- API Tests ----------

func TestAPIChangePlan_Unauthorized(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{"plan_id": "starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	api.handleChangePlan(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIChangePlan_InvalidJSON(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString("not json"))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handleChangePlan(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIChangePlan_MissingPlanID(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{"interval": "monthly"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handleChangePlan(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "missing_plan_id", resp["code"])
}

func TestAPIChangePlan_UpgradeSuccess(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{"plan_id": "starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handleChangePlan(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result PlanChangeResult
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, DirectionUpgrade, result.Direction)
	assert.Equal(t, PlanStarter, result.NewPlan)
}

func TestAPIChangePlan_AlreadyOnPlan(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	createTestSubscription(t, store, "user1", PlanStarter)
	api := NewPlanChangeAPI(svc)

	body := `{"plan_id": "starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handleChangePlan(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAPIChangePlan_InvalidPlan(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{"plan_id": "nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handleChangePlan(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIPreviewChange_Unauthorized(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{"plan_id": "starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/preview-change", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	api.handlePreviewChange(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIPreviewChange_Success(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{"plan_id": "pro"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/preview-change", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handlePreviewChange(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result PlanChangeResult
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, DirectionUpgrade, result.Direction)
}

func TestAPIPreviewChange_MissingPlanID(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/preview-change", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handlePreviewChange(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIPreviewChange_InvalidJSON(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/preview-change", bytes.NewBufferString("{bad"))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()

	api.handlePreviewChange(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIRegisterRoutes(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)
	api := NewPlanChangeAPI(svc)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)
	// Verify routes are registered by making requests.

	body := `{"plan_id": "starter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/change-plan", bytes.NewBufferString(body))
	req = withUserCtx(req, "user1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- IntervalDefaulting Tests ----------

func TestChangePlan_DefaultInterval(t *testing.T) {
	svc, _ := newTestPlanChangeService(t)

	result, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanStarter})
	require.NoError(t, err)
	// Default interval should be monthly when no subscription exists.
	assert.Equal(t, IntervalMonthly, result.Subscription.BillingInterval)
}

func TestChangePlan_PreservesExistingInterval(t *testing.T) {
	svc, store := newTestPlanChangeService(t)
	now := time.Now().UTC()
	sub := &Subscription{
		ID:                 "sub-yearly",
		UserID:             "user1",
		PlanID:             PlanStarter,
		Status:             SubStatusActive,
		BillingInterval:    IntervalYearly,
		CurrentPeriodStart: now.AddDate(0, 0, -15),
		CurrentPeriodEnd:   now.AddDate(0, 0, 15),
	}
	require.NoError(t, store.Create(sub))

	result, err := svc.ChangePlan("user1", PlanChangeRequest{NewPlanID: PlanPro})
	require.NoError(t, err)
	// Should preserve yearly interval from existing subscription.
	assert.Equal(t, IntervalYearly, result.Subscription.BillingInterval)
}
