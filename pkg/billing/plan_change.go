package billing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlanChangeDirection indicates whether a plan change is an upgrade or downgrade.
type PlanChangeDirection string

const (
	DirectionUpgrade   PlanChangeDirection = "upgrade"
	DirectionDowngrade PlanChangeDirection = "downgrade"
	DirectionSame      PlanChangeDirection = "same"
)

// PlanChangeMode indicates when the plan change takes effect.
type PlanChangeMode string

const (
	// ModeImmediate applies the plan change right away (typically upgrades).
	ModeImmediate PlanChangeMode = "immediate"
	// ModeAtPeriodEnd applies the plan change at the end of the current billing period (typically downgrades).
	ModeAtPeriodEnd PlanChangeMode = "at_period_end"
)

// PlanChangeRequest is the input for a plan change operation.
type PlanChangeRequest struct {
	// NewPlanID is the plan to switch to.
	NewPlanID PlanID `json:"plan_id"`
	// Interval is the desired billing interval (monthly/yearly). Optional; defaults to current.
	Interval BillingInterval `json:"interval,omitempty"`
	// Mode overrides the default change mode. If empty, upgrades are immediate and downgrades are at period end.
	Mode PlanChangeMode `json:"mode,omitempty"`
}

// PlanChangeResult describes the outcome of a plan change.
type PlanChangeResult struct {
	// Direction indicates whether this is an upgrade, downgrade, or same.
	Direction PlanChangeDirection `json:"direction"`
	// Mode indicates when the change takes/took effect.
	Mode PlanChangeMode `json:"mode"`
	// PreviousPlan is the plan the user was on.
	PreviousPlan PlanID `json:"previous_plan"`
	// NewPlan is the plan the user is switching to.
	NewPlan PlanID `json:"new_plan"`
	// EffectiveAt is when the plan change takes effect.
	EffectiveAt time.Time `json:"effective_at"`
	// Subscription is the updated subscription record.
	Subscription *Subscription `json:"subscription"`
	// ProrationAmount is the estimated proration in cents (positive = charge, negative = credit).
	// Only set for immediate changes when calculable.
	ProrationAmount int64 `json:"proration_amount,omitempty"`
}

// PlanChangeService handles plan upgrades and downgrades with proration logic.
type PlanChangeService struct {
	subStore  SubscriptionStore
	catalogue *Catalogue
	priceMap  *PlanPriceMap
	stripe    *StripeClient // nil if Stripe not configured
	// OnPlanChanged is an optional callback invoked after a successful plan change.
	OnPlanChanged func(userID string, result *PlanChangeResult)
}

// PlanChangeConfig holds dependencies for creating a PlanChangeService.
type PlanChangeConfig struct {
	SubStore  SubscriptionStore
	Catalogue *Catalogue
	PriceMap  *PlanPriceMap
	Stripe    *StripeClient
}

// NewPlanChangeService creates a PlanChangeService with the given dependencies.
func NewPlanChangeService(cfg PlanChangeConfig) (*PlanChangeService, error) {
	if cfg.SubStore == nil {
		return nil, fmt.Errorf("billing: subscription store is required")
	}
	if cfg.Catalogue == nil {
		return nil, fmt.Errorf("billing: catalogue is required")
	}
	return &PlanChangeService{
		subStore:  cfg.SubStore,
		catalogue: cfg.Catalogue,
		priceMap:  cfg.PriceMap,
		stripe:    cfg.Stripe,
	}, nil
}

// PlanTierOrder returns the tier ordering index for a plan (higher = more expensive).
func PlanTierOrder(id PlanID) int {
	switch id {
	case PlanFree:
		return 0
	case PlanStarter:
		return 1
	case PlanPro:
		return 2
	case PlanEnterprise:
		return 3
	default:
		return -1
	}
}

// ComparePlans returns the direction of changing from oldPlan to newPlan.
func ComparePlans(oldPlan, newPlan PlanID) PlanChangeDirection {
	oldTier := PlanTierOrder(oldPlan)
	newTier := PlanTierOrder(newPlan)
	if newTier > oldTier {
		return DirectionUpgrade
	}
	if newTier < oldTier {
		return DirectionDowngrade
	}
	return DirectionSame
}

// DefaultChangeMode returns the default mode for a given change direction.
func DefaultChangeMode(dir PlanChangeDirection) PlanChangeMode {
	switch dir {
	case DirectionUpgrade:
		return ModeImmediate
	case DirectionDowngrade:
		return ModeAtPeriodEnd
	default:
		return ModeImmediate
	}
}

// ChangePlan processes a plan change for the given user.
func (s *PlanChangeService) ChangePlan(userID string, req PlanChangeRequest) (*PlanChangeResult, error) {
	if userID == "" {
		return nil, fmt.Errorf("billing: user ID is required")
	}

	// Validate the target plan.
	if !ValidPlanID(req.NewPlanID) {
		return nil, fmt.Errorf("billing: invalid plan ID %q", req.NewPlanID)
	}
	newPlan := s.catalogue.Get(req.NewPlanID)
	if newPlan == nil {
		return nil, fmt.Errorf("billing: plan %q not found", req.NewPlanID)
	}
	if !newPlan.Active {
		return nil, fmt.Errorf("billing: plan %q is not available", req.NewPlanID)
	}

	// Get the user's current subscription.
	sub, err := s.subStore.GetByUserID(userID)
	if err != nil {
		// No subscription = free plan user.
		sub = nil
	}

	currentPlanID := PlanFree
	if sub != nil {
		currentPlanID = sub.PlanID
	}

	// Determine direction.
	direction := ComparePlans(currentPlanID, req.NewPlanID)
	if direction == DirectionSame {
		return nil, fmt.Errorf("billing: already on plan %q", req.NewPlanID)
	}

	// Determine mode.
	mode := req.Mode
	if mode == "" {
		mode = DefaultChangeMode(direction)
	}
	if mode != ModeImmediate && mode != ModeAtPeriodEnd {
		return nil, fmt.Errorf("billing: invalid change mode %q", mode)
	}

	// Determine billing interval.
	interval := req.Interval
	if interval == "" && sub != nil {
		interval = sub.BillingInterval
	}
	if interval == "" || interval == IntervalNone {
		interval = IntervalMonthly
	}

	now := time.Now().UTC()
	result := &PlanChangeResult{
		Direction:    direction,
		Mode:         mode,
		PreviousPlan: currentPlanID,
		NewPlan:      req.NewPlanID,
	}

	// Handle downgrade to free plan (cancel subscription).
	if req.NewPlanID == PlanFree {
		return s.handleDowngradeToFree(sub, mode, now, result)
	}

	// Handle upgrade from free plan (create new subscription).
	if currentPlanID == PlanFree || sub == nil {
		return s.handleUpgradeFromFree(userID, req.NewPlanID, interval, now, result)
	}

	// Handle paid → paid plan change.
	return s.handlePaidPlanChange(sub, req.NewPlanID, interval, direction, mode, now, result)
}

// handleDowngradeToFree cancels the subscription (immediately or at period end).
func (s *PlanChangeService) handleDowngradeToFree(sub *Subscription, mode PlanChangeMode, now time.Time, result *PlanChangeResult) (*PlanChangeResult, error) {
	if sub == nil {
		return nil, fmt.Errorf("billing: no subscription to cancel")
	}

	if mode == ModeImmediate {
		// Immediate cancellation.
		if s.stripe != nil && sub.StripeSubID != "" {
			if err := s.stripe.CancelSubscriptionImmediately(sub.StripeSubID); err != nil {
				return nil, fmt.Errorf("billing: stripe cancel: %w", err)
			}
		}
		sub.Status = SubStatusCanceled
		sub.PlanID = PlanFree
		sub.CancelAtPeriodEnd = false
		result.EffectiveAt = now
	} else {
		// Cancel at period end.
		if s.stripe != nil && sub.StripeSubID != "" {
			if _, err := s.stripe.CancelSubscription(sub.StripeSubID); err != nil {
				return nil, fmt.Errorf("billing: stripe cancel at period end: %w", err)
			}
		}
		sub.CancelAtPeriodEnd = true
		result.EffectiveAt = sub.CurrentPeriodEnd
	}

	if err := s.subStore.Update(sub); err != nil {
		return nil, fmt.Errorf("billing: update subscription: %w", err)
	}

	result.Subscription = sub
	s.notifyChanged(sub.UserID, result)
	return result, nil
}

// handleUpgradeFromFree creates a new paid subscription.
func (s *PlanChangeService) handleUpgradeFromFree(userID string, newPlanID PlanID, interval BillingInterval, now time.Time, result *PlanChangeResult) (*PlanChangeResult, error) {
	periodEnd := now.AddDate(0, 1, 0)
	if interval == IntervalYearly {
		periodEnd = now.AddDate(1, 0, 0)
	}

	sub := &Subscription{
		ID:                 uuid.New().String(),
		UserID:             userID,
		PlanID:             newPlanID,
		Status:             SubStatusActive,
		BillingInterval:    interval,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   periodEnd,
	}

	if err := s.subStore.Create(sub); err != nil {
		return nil, fmt.Errorf("billing: create subscription: %w", err)
	}

	result.EffectiveAt = now
	result.Mode = ModeImmediate // upgrades from free are always immediate
	result.Subscription = sub
	s.notifyChanged(userID, result)
	return result, nil
}

// handlePaidPlanChange handles transitions between paid plans.
func (s *PlanChangeService) handlePaidPlanChange(sub *Subscription, newPlanID PlanID, interval BillingInterval, direction PlanChangeDirection, mode PlanChangeMode, now time.Time, result *PlanChangeResult) (*PlanChangeResult, error) {
	// Calculate proration for informational purposes.
	result.ProrationAmount = s.estimateProration(sub, newPlanID, interval, now)

	if mode == ModeImmediate {
		// Immediate plan change.
		if s.stripe != nil && sub.StripeSubID != "" {
			if err := s.stripeUpdateSubscription(sub.StripeSubID, newPlanID, interval, true); err != nil {
				return nil, fmt.Errorf("billing: stripe update: %w", err)
			}
		}

		sub.PlanID = newPlanID
		sub.BillingInterval = interval
		sub.CancelAtPeriodEnd = false
		result.EffectiveAt = now
	} else {
		// Schedule change at period end.
		result.EffectiveAt = sub.CurrentPeriodEnd

		// For Stripe-managed subscriptions, use Stripe's scheduled update (no proration).
		if s.stripe != nil && sub.StripeSubID != "" {
			if err := s.stripeUpdateSubscription(sub.StripeSubID, newPlanID, interval, false); err != nil {
				// Fall back to local tracking.
				sub.PlanID = newPlanID
				sub.BillingInterval = interval
			}
		} else {
			// Without Stripe, apply immediately (self-hosted mode).
			sub.PlanID = newPlanID
			sub.BillingInterval = interval
			result.EffectiveAt = now
			result.Mode = ModeImmediate
		}
	}

	if err := s.subStore.Update(sub); err != nil {
		return nil, fmt.Errorf("billing: update subscription: %w", err)
	}

	result.Subscription = sub
	s.notifyChanged(sub.UserID, result)
	return result, nil
}

// estimateProration calculates a rough proration amount in cents.
// Positive = additional charge, negative = credit.
func (s *PlanChangeService) estimateProration(sub *Subscription, newPlanID PlanID, newInterval BillingInterval, now time.Time) int64 {
	oldPlan := s.catalogue.Get(sub.PlanID)
	newPlan := s.catalogue.Get(newPlanID)
	if oldPlan == nil || newPlan == nil {
		return 0
	}

	// Get monthly prices.
	oldPrice := oldPlan.PriceMonthly
	newPrice := newPlan.PriceMonthly
	if sub.BillingInterval == IntervalYearly {
		oldPrice = oldPlan.PriceYearly / 12
	}
	if newInterval == IntervalYearly {
		newPrice = newPlan.PriceYearly / 12
	}

	// Calculate remaining period fraction.
	totalDuration := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart)
	if totalDuration <= 0 {
		return newPrice - oldPrice
	}
	remainingDuration := sub.CurrentPeriodEnd.Sub(now)
	if remainingDuration <= 0 {
		return 0 // period already ended
	}

	fraction := float64(remainingDuration) / float64(totalDuration)

	// Proration: (new_price - old_price) * remaining_fraction
	return int64(float64(newPrice-oldPrice) * fraction)
}

// stripeUpdateSubscription updates a Stripe subscription to a new plan.
// If prorate is true, Stripe creates proration invoice items.
func (s *PlanChangeService) stripeUpdateSubscription(stripeSubID string, newPlanID PlanID, interval BillingInterval, prorate bool) error {
	if s.priceMap == nil {
		return fmt.Errorf("billing: no price map configured")
	}
	priceID := s.priceMap.GetPrice(newPlanID, interval)
	if priceID == "" {
		return fmt.Errorf("billing: no Stripe price for plan %s/%s", newPlanID, interval)
	}

	// Fetch the current subscription to get the item ID.
	stripeSub, err := s.stripe.GetSubscription(stripeSubID)
	if err != nil {
		return fmt.Errorf("billing: get stripe subscription: %w", err)
	}

	if stripeSub.Items == nil || len(stripeSub.Items.Data) == 0 {
		return fmt.Errorf("billing: subscription has no items")
	}

	itemID := stripeSub.Items.Data[0].ID

	// Build update params.
	params := url.Values{}
	params.Set("items[0][id]", itemID)
	params.Set("items[0][price]", priceID)
	if prorate {
		params.Set("proration_behavior", "create_prorations")
	} else {
		params.Set("proration_behavior", "none")
	}

	// POST to update the subscription.
	body := strings.NewReader(params.Encode())
	req, err := http.NewRequest(http.MethodPost, s.stripe.baseURL+"/v1/subscriptions/"+stripeSubID, body)
	if err != nil {
		return fmt.Errorf("billing: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var updatedSub StripeSubscription
	if err := s.stripe.do(req, &updatedSub); err != nil {
		return fmt.Errorf("billing: update subscription: %w", err)
	}

	return nil
}

// notifyChanged invokes the OnPlanChanged callback if set.
func (s *PlanChangeService) notifyChanged(userID string, result *PlanChangeResult) {
	if s.OnPlanChanged != nil {
		s.OnPlanChanged(userID, result)
	}
}

// Preview returns what would happen if a plan change were executed,
// without actually making any changes.
func (s *PlanChangeService) Preview(userID string, req PlanChangeRequest) (*PlanChangeResult, error) {
	if userID == "" {
		return nil, fmt.Errorf("billing: user ID is required")
	}

	if !ValidPlanID(req.NewPlanID) {
		return nil, fmt.Errorf("billing: invalid plan ID %q", req.NewPlanID)
	}
	newPlan := s.catalogue.Get(req.NewPlanID)
	if newPlan == nil {
		return nil, fmt.Errorf("billing: plan %q not found", req.NewPlanID)
	}

	sub, _ := s.subStore.GetByUserID(userID)
	currentPlanID := PlanFree
	if sub != nil {
		currentPlanID = sub.PlanID
	}

	direction := ComparePlans(currentPlanID, req.NewPlanID)
	mode := req.Mode
	if mode == "" {
		mode = DefaultChangeMode(direction)
	}

	interval := req.Interval
	if interval == "" && sub != nil {
		interval = sub.BillingInterval
	}
	if interval == "" || interval == IntervalNone {
		interval = IntervalMonthly
	}

	now := time.Now().UTC()
	result := &PlanChangeResult{
		Direction:    direction,
		Mode:         mode,
		PreviousPlan: currentPlanID,
		NewPlan:      req.NewPlanID,
	}

	if mode == ModeImmediate {
		result.EffectiveAt = now
	} else if sub != nil {
		result.EffectiveAt = sub.CurrentPeriodEnd
	} else {
		result.EffectiveAt = now
	}

	if sub != nil && direction != DirectionSame {
		result.ProrationAmount = s.estimateProration(sub, req.NewPlanID, interval, now)
	}

	return result, nil
}

// ---------- Plan Change API ----------

// PlanChangeAPI provides HTTP handlers for plan upgrade/downgrade.
type PlanChangeAPI struct {
	service *PlanChangeService
}

// NewPlanChangeAPI creates a PlanChangeAPI.
func NewPlanChangeAPI(service *PlanChangeService) *PlanChangeAPI {
	return &PlanChangeAPI{service: service}
}

// RegisterRoutes registers plan change routes on the given mux.
func (a *PlanChangeAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/billing/change-plan", a.handleChangePlan)
	mux.HandleFunc("POST /api/v1/billing/preview-change", a.handlePreviewChange)
}

// handleChangePlan processes a plan upgrade or downgrade.
func (a *PlanChangeAPI) handleChangePlan(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "authentication required",
			"code":  "unauthorized",
		})
		return
	}

	var req PlanChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid JSON",
			"code":  "invalid_request",
		})
		return
	}

	if req.NewPlanID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "plan_id is required",
			"code":  "missing_plan_id",
		})
		return
	}

	result, err := a.service.ChangePlan(userID, req)
	if err != nil {
		// Map specific errors to HTTP status codes.
		code := http.StatusInternalServerError
		errCode := "internal_error"
		msg := err.Error()

		if strings.Contains(msg, "invalid plan") {
			code = http.StatusBadRequest
			errCode = "invalid_plan"
		} else if strings.Contains(msg, "not found") {
			code = http.StatusNotFound
			errCode = "plan_not_found"
		} else if strings.Contains(msg, "not available") {
			code = http.StatusBadRequest
			errCode = "plan_not_available"
		} else if strings.Contains(msg, "already on plan") {
			code = http.StatusConflict
			errCode = "already_on_plan"
		} else if strings.Contains(msg, "invalid change mode") {
			code = http.StatusBadRequest
			errCode = "invalid_mode"
		} else if strings.Contains(msg, "no subscription to cancel") {
			code = http.StatusBadRequest
			errCode = "no_subscription"
		} else if strings.Contains(msg, "stripe") {
			code = http.StatusBadGateway
			errCode = "stripe_error"
		}

		writeJSON(w, code, map[string]any{
			"error": msg,
			"code":  errCode,
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handlePreviewChange returns what would happen for a plan change without executing it.
func (a *PlanChangeAPI) handlePreviewChange(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "authentication required",
			"code":  "unauthorized",
		})
		return
	}

	var req PlanChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid JSON",
			"code":  "invalid_request",
		})
		return
	}

	if req.NewPlanID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "plan_id is required",
			"code":  "missing_plan_id",
		})
		return
	}

	result, err := a.service.Preview(userID, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"code":  "preview_error",
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}
