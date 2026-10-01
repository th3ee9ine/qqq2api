package domain

// These data shapes remain for reading historical announcement rows through Ent.
// Announcement publishing and targeting evaluation have been retired.
const (
	AnnouncementStatusDraft      = "draft"
	AnnouncementNotifyModeSilent = "silent"
)

type AnnouncementTargeting struct {
	AnyOf []AnnouncementConditionGroup `json:"any_of,omitempty"`
}

type AnnouncementConditionGroup struct {
	AllOf []AnnouncementCondition `json:"all_of,omitempty"`
}

type AnnouncementCondition struct {
	Type     string  `json:"type"`
	Operator string  `json:"operator"`
	GroupIDs []int64 `json:"group_ids,omitempty"`
	Value    float64 `json:"value,omitempty"`
}
