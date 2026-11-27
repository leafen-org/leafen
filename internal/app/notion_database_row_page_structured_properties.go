package app

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"time"
)

type notionDatabaseRowPagePlaceValue struct {
	Lat           *float64 `json:"lat"`
	Lon           *float64 `json:"lon"`
	Name          *string  `json:"name"`
	Address       *string  `json:"address"`
	AWSPlaceID    *string  `json:"aws_place_id"`
	GooglePlaceID *string  `json:"google_place_id"`
}

type notionDatabaseRowPageVerificationDate struct {
	Start    string  `json:"start"`
	End      *string `json:"end"`
	TimeZone any     `json:"time_zone"`
}

type notionDatabaseRowPageVerificationValue struct {
	State      string                                 `json:"state"`
	Date       *notionDatabaseRowPageVerificationDate `json:"date"`
	VerifiedBy string                                 `json:"verified_by"`
}

func notionDatabaseRowPagePlace(raw json.RawMessage) (map[string]any, error) {
	value := notionDatabaseRowPagePlaceValue{}
	if decodeNotionDatabaseRowPageStructured(raw, &value) != nil || value.Lat == nil || value.Lon == nil ||
		math.IsNaN(*value.Lat) || math.IsInf(*value.Lat, 0) || *value.Lat < -90 || *value.Lat > 90 ||
		math.IsNaN(*value.Lon) || math.IsInf(*value.Lon, 0) || *value.Lon < -180 || *value.Lon > 180 ||
		!notionDatabaseRowPageOptionalPlaceText(value.Name) ||
		!notionDatabaseRowPageOptionalPlaceText(value.Address) ||
		!notionDatabaseRowPageOptionalPlaceText(value.AWSPlaceID) ||
		!notionDatabaseRowPageOptionalPlaceText(value.GooglePlaceID) {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	return map[string]any{
		"lat": *value.Lat, "lon": *value.Lon,
		"name":            notionDatabaseRowPageOptionalPlaceValue(value.Name),
		"address":         notionDatabaseRowPageOptionalPlaceValue(value.Address),
		"aws_place_id":    notionDatabaseRowPageOptionalPlaceValue(value.AWSPlaceID),
		"google_place_id": notionDatabaseRowPageOptionalPlaceValue(value.GooglePlaceID),
	}, nil
}

func notionDatabaseRowPageOptionalPlaceText(value *string) bool {
	return value == nil || len([]rune(*value)) <= 2000
}

func notionDatabaseRowPageOptionalPlaceValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func notionDatabaseRowPageVerification(
	raw json.RawMessage,
	now time.Time,
) (map[string]any, error) {
	value := notionDatabaseRowPageVerificationValue{}
	if decodeNotionDatabaseRowPageStructured(raw, &value) != nil ||
		value.State != "verified" && value.State != "unverified" && value.State != "expired" {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	date, end, err := notionDatabaseRowPageVerificationDateValue(value.Date)
	if err != nil || value.State == "unverified" && value.Date != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	state := value.State
	if state == "verified" && end != nil && !now.Before(*end) {
		state = "expired"
	}
	var verifiedBy any
	if value.VerifiedBy != "" {
		verifiedBy, err = notionDatabaseRowPagePropertyUser(value.VerifiedBy)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"state": state, "date": date, "verified_by": verifiedBy,
	}, nil
}

func notionDatabaseRowPageVerificationRequest(
	raw json.RawMessage,
	principalID string,
) (map[string]any, error) {
	request := struct {
		State      string                                 `json:"state"`
		Date       *notionDatabaseRowPageVerificationDate `json:"date"`
		VerifiedBy json.RawMessage                        `json:"verified_by"`
	}{}
	if decodeNotionDatabaseRowPageStructured(raw, &request) != nil || len(request.VerifiedBy) != 0 ||
		request.State != "verified" && request.State != "unverified" {
		return nil, notionDatabaseRowPageRequestInvalid("verification value is invalid")
	}
	date, _, err := notionDatabaseRowPageVerificationDateValue(request.Date)
	if err != nil || request.State == "unverified" && request.Date != nil {
		return nil, notionDatabaseRowPageRequestInvalid("verification value is invalid")
	}
	result := map[string]any{"state": request.State}
	if request.State == "verified" {
		result["date"] = date
		result["verified_by"] = principalID
	}
	return result, nil
}

func notionDatabaseRowPageVerificationDateValue(
	value *notionDatabaseRowPageVerificationDate,
) (any, *time.Time, error) {
	if value == nil {
		return nil, nil, nil
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(value.Start))
	if err != nil || value.TimeZone != nil || !strings.Contains(value.Start, "T") {
		return nil, nil, errNotionDatabaseRowPageProjectionInvalid
	}
	var endTime *time.Time
	var end any
	if value.End != nil {
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(*value.End))
		if parseErr != nil || !strings.Contains(*value.End, "T") || parsed.Before(start) {
			return nil, nil, errNotionDatabaseRowPageProjectionInvalid
		}
		endTime = &parsed
		end = *value.End
	}
	return notionDateObject{"start": value.Start, "end": end, "time_zone": nil}, endTime, nil
}

func decodeNotionDatabaseRowPageStructured(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errNotionDatabaseRowPageProjectionInvalid
	}
	return nil
}
