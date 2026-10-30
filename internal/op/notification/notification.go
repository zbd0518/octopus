package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/alert"
	pluginevent "github.com/lingyuins/octopus/internal/plugin/event"
	"gorm.io/gorm"
)

var timeNow = func() int64 { return time.Now().UnixMilli() }

type ListFilter struct {
	Type      string
	Severity  string
	Source    string
	Read      *bool
	Archived  *bool
	Search    string
	StartTime *int64
	EndTime   *int64
}

type CreateOptions struct {
	DeliverExternal bool
	ChannelIDs      []int
}

func Create(ctx context.Context, n *model.Notification) error {
	return CreateWithOptions(ctx, n, CreateOptions{})
}

func CreateWithOptions(ctx context.Context, n *model.Notification, opts CreateOptions) error {
	if n == nil {
		return fmt.Errorf("notification is nil")
	}
	n.Type = normalizeType(n.Type)
	n.Severity = normalizeSeverity(n.Severity)
	if strings.TrimSpace(n.Title) == "" {
		return fmt.Errorf("notification title cannot be empty")
	}
	if err := db.GetDB().WithContext(ctx).Create(n).Error; err != nil {
		return err
	}
	Publish(*n)
	pluginevent.Publish("notification.created", map[string]any{"id": n.ID, "source": n.Source, "severity": n.Severity, "title": n.Title, "content": n.Content, "created_at": n.CreatedAt})
	if opts.DeliverExternal {
		_ = DispatchExternal(ctx, n, opts.ChannelIDs)
	}
	return nil
}

func List(ctx context.Context, filter ListFilter, page, pageSize int) ([]model.Notification, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	q := applyListFilter(db.GetDB().WithContext(ctx).Model(&model.Notification{}), filter)
	var items []model.Notification
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func Get(ctx context.Context, id int64) (*model.Notification, error) {
	var n model.Notification
	if err := db.GetDB().WithContext(ctx).First(&n, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &n, nil
}

func UnreadCount(ctx context.Context, archived bool) (int64, error) {
	q := db.GetDB().WithContext(ctx).Model(&model.Notification{}).Where("read_at IS NULL")
	if !archived {
		q = q.Where("archived_at IS NULL")
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func MarkRead(ctx context.Context, ids []int64) error {
	return updateReadAt(ctx, ids, ptrInt64(timeNow()))
}

func MarkUnread(ctx context.Context, ids []int64) error {
	return updateReadAt(ctx, ids, nil)
}

func MarkAllRead(ctx context.Context) error {
	now := timeNow()
	return db.GetDB().WithContext(ctx).Model(&model.Notification{}).Where("read_at IS NULL").Update("read_at", now).Error
}

func Archive(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := timeNow()
	return db.GetDB().WithContext(ctx).Model(&model.Notification{}).Where("id IN ?", ids).Update("archived_at", now).Error
}

func Unarchive(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).Model(&model.Notification{}).Where("id IN ?", ids).Update("archived_at", nil).Error
}

func Delete(ctx context.Context, id int64) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("notification_id = ?", id).Delete(&model.NotificationDelivery{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&model.Notification{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("notification not found")
		}
		return nil
	})
}

func DeleteArchived(ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := tx.Model(&model.Notification{}).Where("archived_at IS NOT NULL").Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Where("notification_id IN ?", ids).Delete(&model.NotificationDelivery{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&model.Notification{}).Error
	})
}

func DeliveryList(ctx context.Context, notificationID int64) ([]model.NotificationDelivery, error) {
	var items []model.NotificationDelivery
	q := db.GetDB().WithContext(ctx).Order("created_at DESC")
	if notificationID > 0 {
		q = q.Where("notification_id = ?", notificationID)
	}
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func PreferenceList(ctx context.Context) ([]model.NotificationPreference, error) {
	var items []model.NotificationPreference
	if err := db.GetDB().WithContext(ctx).Order("type ASC, user_id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func PreferenceSave(ctx context.Context, pref *model.NotificationPreference) error {
	if pref == nil {
		return fmt.Errorf("notification preference is nil")
	}
	pref.Type = normalizeType(pref.Type)
	pref.MinSeverity = normalizeSeverity(pref.MinSeverity)
	return db.GetDB().WithContext(ctx).Save(pref).Error
}
func PreferenceDelete(ctx context.Context, id int64) error {
	res := db.GetDB().WithContext(ctx).Delete(&model.NotificationPreference{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("notification preference not found")
	}
	return nil
}

func PolicyList(ctx context.Context) ([]model.NotificationPolicy, error) {
	var items []model.NotificationPolicy
	if err := db.GetDB().WithContext(ctx).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func PolicyCreate(ctx context.Context, p *model.NotificationPolicy) error {
	if p == nil {
		return fmt.Errorf("notification policy is nil")
	}
	p.ID = 0
	p.Type = normalizeTypeAllowEmpty(p.Type)
	p.MinSeverity = normalizeSeverity(p.MinSeverity)
	return db.GetDB().WithContext(ctx).Create(p).Error
}

func PolicyUpdate(ctx context.Context, p *model.NotificationPolicy) error {
	if p == nil || p.ID == 0 {
		return fmt.Errorf("notification policy not found")
	}
	p.Type = normalizeTypeAllowEmpty(p.Type)
	p.MinSeverity = normalizeSeverity(p.MinSeverity)
	res := db.GetDB().WithContext(ctx).Save(p)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("notification policy not found")
	}
	return nil
}

func PolicyDelete(ctx context.Context, id int64) error {
	res := db.GetDB().WithContext(ctx).Delete(&model.NotificationPolicy{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("notification policy not found")
	}
	return nil
}

func DispatchExternal(ctx context.Context, n *model.Notification, explicitChannelIDs []int) error {
	if n == nil || n.ID == 0 {
		return fmt.Errorf("notification must be persisted before dispatch")
	}
	channelIDs := explicitChannelIDs
	if len(channelIDs) == 0 {
		policies, err := PolicyList(ctx)
		if err != nil {
			return err
		}
		channelIDs = matchPolicyChannelIDs(n, policies)
	}
	channelIDs = uniqueInts(channelIDs)
	if len(channelIDs) == 0 {
		return nil
	}
	channels, err := alert.NotifChannelList(ctx)
	if err != nil {
		return err
	}
	channelMap := make(map[int]model.AlertNotifChannel, len(channels))
	for _, ch := range channels {
		channelMap[ch.ID] = ch
	}
	for _, id := range channelIDs {
		ch, ok := channelMap[id]
		d := model.NotificationDelivery{NotificationID: n.ID, ChannelID: id, Attempts: 1}
		if !ok {
			d.Status = model.NotificationDeliverySkipped
			d.LastError = "notification channel not found"
			_ = db.GetDB().WithContext(ctx).Create(&d).Error
			continue
		}
		d.ChannelName = ch.Name
		d.ChannelType = ch.Type
		if err := helper.SendNotificationMessage(&ch, n.Title, n.Content); err != nil {
			d.Status = model.NotificationDeliveryFailed
			d.LastError = err.Error()
		} else {
			now := timeNow()
			d.Status = model.NotificationDeliverySent
			d.SentAt = &now
		}
		_ = db.GetDB().WithContext(ctx).Create(&d).Error
	}
	return nil
}

func applyListFilter(q *gorm.DB, filter ListFilter) *gorm.DB {
	if filter.Type != "" {
		q = q.Where("type = ?", filter.Type)
	}
	if filter.Severity != "" {
		q = q.Where("severity = ?", filter.Severity)
	}
	if filter.Source != "" {
		q = q.Where("source = ?", filter.Source)
	}
	if filter.Read != nil {
		if *filter.Read {
			q = q.Where("read_at IS NOT NULL")
		} else {
			q = q.Where("read_at IS NULL")
		}
	}
	if filter.Archived != nil {
		if *filter.Archived {
			q = q.Where("archived_at IS NOT NULL")
		} else {
			q = q.Where("archived_at IS NULL")
		}
	} else {
		q = q.Where("archived_at IS NULL")
	}
	if filter.Search != "" {
		like := "%" + filter.Search + "%"
		q = q.Where("title LIKE ? OR content LIKE ?", like, like)
	}
	if filter.StartTime != nil {
		q = q.Where("created_at >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		q = q.Where("created_at <= ?", *filter.EndTime)
	}
	return q
}

func updateReadAt(ctx context.Context, ids []int64, readAt *int64) error {
	if len(ids) == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).Model(&model.Notification{}).Where("id IN ?", ids).Update("read_at", readAt).Error
}

func ptrInt64(v int64) *int64 { return &v }

func normalizeType(t model.NotificationType) model.NotificationType {
	if strings.TrimSpace(string(t)) == "" {
		return model.NotificationTypeSystem
	}
	return t
}

func normalizeTypeAllowEmpty(t model.NotificationType) model.NotificationType { return t }

func normalizeSeverity(s model.NotificationSeverity) model.NotificationSeverity {
	switch s {
	case model.NotificationSeveritySuccess, model.NotificationSeverityWarning, model.NotificationSeverityError, model.NotificationSeverityCritical:
		return s
	default:
		return model.NotificationSeverityInfo
	}
}

func severityRank(s model.NotificationSeverity) int {
	switch normalizeSeverity(s) {
	case model.NotificationSeveritySuccess:
		return 1
	case model.NotificationSeverityWarning:
		return 2
	case model.NotificationSeverityError:
		return 3
	case model.NotificationSeverityCritical:
		return 4
	default:
		return 0
	}
}

func matchPolicyChannelIDs(n *model.Notification, policies []model.NotificationPolicy) []int {
	ids := make([]int, 0)
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		if p.Type != "" && p.Type != n.Type {
			continue
		}
		if p.Source != "" && p.Source != n.Source {
			continue
		}
		if severityRank(n.Severity) < severityRank(p.MinSeverity) {
			continue
		}
		ids = append(ids, parseChannelIDs(p.ChannelIDs)...)
	}
	return ids
}

func parseChannelIDs(raw string) []int {
	var ids []int
	if strings.TrimSpace(raw) == "" {
		return ids
	}
	if err := json.Unmarshal([]byte(raw), &ids); err == nil {
		return ids
	}
	for _, part := range strings.Split(raw, ",") {
		var id int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &id); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func uniqueInts(in []int) []int {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}
