package eiszhs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

// Normalizer translates validated ЕИСЖС transport payloads into domain-neutral current data.
type Normalizer struct{}

// NewNormalizer creates the adapter-owned current response normalizer.
func NewNormalizer() Normalizer {
	return Normalizer{}
}

// NormalizeComplexMembership validates one authoritative membership page.
//
//nolint:funlen,gocyclo // Each branch rejects one integrity violation before neutral output.
func (Normalizer) NormalizeComplexMembership(raw []byte) (application.CurrentMembership, error) {
	var payload struct {
		Total      *int64 `json:"total"`
		LimitItems *int   `json:"limitItems"`
		Elements   *struct {
			Objects []struct {
				ID *int64 `json:"objectId"`
			} `json:"constructionObjectShortInfo"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return application.CurrentMembership{}, fmt.Errorf("decode ЕИСЖС complex membership: %w", err)
	}
	if payload.Total == nil || payload.LimitItems == nil || payload.Elements == nil ||
		*payload.Total < 0 || *payload.LimitItems != pageSize ||
		int64(len(payload.Elements.Objects)) > *payload.Total {
		return application.CurrentMembership{}, errors.New(
			"ЕИСЖС complex membership has invalid pagination",
		)
	}
	result := application.CurrentMembership{Total: *payload.Total, PageSize: *payload.LimitItems}
	for _, object := range payload.Elements.Objects {
		if object.ID == nil || *object.ID <= 0 {
			return application.CurrentMembership{}, errors.New(
				"ЕИСЖС complex membership lacks a stable object ID",
			)
		}
		id := strconv.FormatInt(*object.ID, 10)
		result.ObjectIDs = append(result.ObjectIDs, id)
	}
	return result, nil
}

// NormalizeCurrentExtended requires all supported cumulative categories for the requested object.
func (Normalizer) NormalizeCurrentExtended(
	raw []byte,
	expectedObjectID string,
) ([]application.CurrentCategoryMetrics, error) {
	var payload struct {
		ObjectID *int64 `json:"objectId"`
		DDUInfo  *struct {
			Living    *int64 `json:"dduLivNumber"`
			NonLiving *int64 `json:"dduNonlivNumber"`
			Parking   *int64 `json:"dduParkingNumber"`
		} `json:"dduInfo"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode ЕИСЖС current extended object: %w", err)
	}
	if payload.ObjectID == nil || strconv.FormatInt(*payload.ObjectID, 10) != expectedObjectID {
		return nil, errors.New("ЕИСЖС current extended object ID does not match the requested object")
	}
	if payload.DDUInfo == nil || payload.DDUInfo.Living == nil ||
		payload.DDUInfo.NonLiving == nil || payload.DDUInfo.Parking == nil {
		return nil, errors.New("ЕИСЖС current extended object lacks a supported cumulative ДДУ category")
	}
	if *payload.DDUInfo.Living < 0 || *payload.DDUInfo.NonLiving < 0 || *payload.DDUInfo.Parking < 0 {
		return nil, errors.New("ЕИСЖС current extended object has invalid cumulative ДДУ values")
	}
	return []application.CurrentCategoryMetrics{
		{SegmentCode: "residential", CumulativeDDUCount: *payload.DDUInfo.Living},
		{SegmentCode: "non_residential", CumulativeDDUCount: *payload.DDUInfo.NonLiving},
		{SegmentCode: "parking", CumulativeDDUCount: *payload.DDUInfo.Parking},
	}, nil
}
