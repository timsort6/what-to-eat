package addresses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"food-agent/pkg/dao"
	"food-agent/pkg/model"

	"gorm.io/gorm"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrPermissionDenied = errors.New("permission denied")
)

type Service interface {
	Create(ctx context.Context, userID uint, req CreateAddressRequest) (*model.UserFavoriteAddress, error)
	List(ctx context.Context, userID uint) ([]model.UserFavoriteAddress, error)
	Update(ctx context.Context, userID uint, id uint, req UpdateAddressRequest) (*model.UserFavoriteAddress, error)
	Delete(ctx context.Context, userID uint, id uint) error
}

type favoriteAddressDAO interface {
	InTx(ctx context.Context, fn func(tx favoriteAddressTx) error) error
	ListByUserID(ctx context.Context, userID uint) ([]model.UserFavoriteAddress, error)
}

type favoriteAddressTx interface {
	CountByUserID(ctx context.Context, userID uint) (int64, error)
	GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error)
	Create(ctx context.Context, addr *model.UserFavoriteAddress) error
	Update(ctx context.Context, addr *model.UserFavoriteAddress) error
	Delete(ctx context.Context, id uint) error

	ClearDefaultByUserID(ctx context.Context, userID uint) error
	FindMinIDByUserID(ctx context.Context, userID uint) (uint, bool, error)
	SetDefaultByID(ctx context.Context, id uint, isDefault bool) error
}

type service struct {
	dao favoriteAddressDAO
}

func NewService(db *gorm.DB) (Service, error) {
	if db == nil {
		return nil, fmt.Errorf("nil db")
	}
	return &service{dao: &gormFavoriteAddressDAO{db: db}}, nil
}

func (s *service) Create(ctx context.Context, userID uint, req CreateAddressRequest) (*model.UserFavoriteAddress, error) {
	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, fmt.Errorf("empty label")
	}
	loc := strings.TrimSpace(req.Location)
	lng, lat, err := parseLocation(loc)
	if err != nil {
		return nil, err
	}

	addr := &model.UserFavoriteAddress{
		UserID:           userID,
		Label:            label,
		IsDefault:        false,
		AmapID:           strings.TrimSpace(req.AmapID),
		Name:             strings.TrimSpace(req.Name),
		Type:             strings.TrimSpace(req.Type),
		TypeCode:         strings.TrimSpace(req.TypeCode),
		Tel:              strings.TrimSpace(req.Tel),
		Tag:              strings.TrimSpace(req.Tag),
		FormattedAddress: strings.TrimSpace(req.FormattedAddress),
		Address:          strings.TrimSpace(req.Address),
		Location:         loc,
		Longitude:        lng,
		Latitude:         lat,
		Pname:            strings.TrimSpace(req.Pname),
		Cityname:         strings.TrimSpace(req.Cityname),
		Adname:           strings.TrimSpace(req.Adname),
		Citycode:         strings.TrimSpace(req.Citycode),
		Adcode:           strings.TrimSpace(req.Adcode),
		RawPOIJSON:       compactRawJSON(req.AMapPOI),
	}

	var created model.UserFavoriteAddress
	if err := s.dao.InTx(ctx, func(tx favoriteAddressTx) error {
		cnt, err := tx.CountByUserID(ctx, userID)
		if err != nil {
			return err
		}

		// Default rules:
		// - If this is the first address for the user => must be default.
		// - Otherwise, only allow "set default" (true); false/omitted means no-op.
		makeDefault := cnt == 0
		if !makeDefault && req.IsDefault != nil && *req.IsDefault {
			makeDefault = true
		}

		if makeDefault {
			if err := tx.ClearDefaultByUserID(ctx, userID); err != nil {
				return err
			}
			addr.IsDefault = true
		}

		if err := tx.Create(ctx, addr); err != nil {
			return err
		}
		created = *addr
		return nil
	}); err != nil {
		return nil, err
	}

	return &created, nil
}

func (s *service) List(ctx context.Context, userID uint) ([]model.UserFavoriteAddress, error) {
	return s.dao.ListByUserID(ctx, userID)
}

func (s *service) Update(ctx context.Context, userID uint, id uint, req UpdateAddressRequest) (*model.UserFavoriteAddress, error) {
	var updated model.UserFavoriteAddress
	if err := s.dao.InTx(ctx, func(tx favoriteAddressTx) error {
		addr, err := tx.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if addr.UserID != userID {
			return ErrPermissionDenied
		}

		if req.Label != nil {
			label := strings.TrimSpace(*req.Label)
			if label == "" {
				return fmt.Errorf("empty label")
			}
			addr.Label = label
		}

		if req.Location != nil {
			loc := strings.TrimSpace(*req.Location)
			lng, lat, err := parseLocation(loc)
			if err != nil {
				return err
			}
			addr.Location = loc
			addr.Longitude = lng
			addr.Latitude = lat
		}

		setString := func(dst *string, v *string) {
			if v == nil {
				return
			}
			*dst = strings.TrimSpace(*v)
		}

		setString(&addr.AmapID, req.AmapID)
		setString(&addr.Name, req.Name)
		setString(&addr.Type, req.Type)
		setString(&addr.TypeCode, req.TypeCode)
		setString(&addr.Tel, req.Tel)
		setString(&addr.Tag, req.Tag)
		setString(&addr.FormattedAddress, req.FormattedAddress)
		setString(&addr.Address, req.Address)
		setString(&addr.Pname, req.Pname)
		setString(&addr.Cityname, req.Cityname)
		setString(&addr.Adname, req.Adname)
		setString(&addr.Citycode, req.Citycode)
		setString(&addr.Adcode, req.Adcode)

		if req.AMapPOI != nil {
			addr.RawPOIJSON = compactRawJSON(*req.AMapPOI)
		}

		// Only supports "set default".
		if req.IsDefault != nil && *req.IsDefault {
			if err := tx.ClearDefaultByUserID(ctx, userID); err != nil {
				return err
			}
			addr.IsDefault = true
		}

		if err := tx.Update(ctx, addr); err != nil {
			return err
		}
		updated = *addr
		return nil
	}); err != nil {
		return nil, err
	}

	return &updated, nil
}

func (s *service) Delete(ctx context.Context, userID uint, id uint) error {
	return s.dao.InTx(ctx, func(tx favoriteAddressTx) error {
		addr, err := tx.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if addr.UserID != userID {
			return ErrPermissionDenied
		}
		wasDefault := addr.IsDefault

		if err := tx.Delete(ctx, id); err != nil {
			return err
		}

		// If deleting default address, migrate default to the minimum id remaining.
		if wasDefault {
			minID, ok, err := tx.FindMinIDByUserID(ctx, userID)
			if err != nil {
				return err
			}
			if ok {
				if err := tx.ClearDefaultByUserID(ctx, userID); err != nil {
					return err
				}
				if err := tx.SetDefaultByID(ctx, minID, true); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func parseLocation(s string) (lng float64, lat float64, err error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid location (require lng,lat)")
	}
	lng, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lng")
	}
	lat, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lat")
	}
	if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return 0, 0, fmt.Errorf("invalid lng: must be between -180 and 180")
	}
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("invalid lat: must be between -90 and 90")
	}
	return lng, lat, nil
}

func compactRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Keep raw text if it's not valid json (best effort, should not happen).
		return strings.TrimSpace(string(raw))
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(b)
}

// --- DAO adapters ---

// gormFavoriteAddressDAO wires addresses.Service to pkg/dao using gorm transactions.
// All table operations live in pkg/dao; this adapter only manages transaction boundaries
// and error mapping (record not found).
type gormFavoriteAddressDAO struct {
	db *gorm.DB
}

func (d *gormFavoriteAddressDAO) InTx(ctx context.Context, fn func(tx favoriteAddressTx) error) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&gormFavoriteAddressDAO{db: tx})
	})
}

func (d *gormFavoriteAddressDAO) ListByUserID(ctx context.Context, userID uint) ([]model.UserFavoriteAddress, error) {
	return dao.NewUserFavoriteAddressDAO(d.db).ListAllByUserID(ctx, userID)
}

func (d *gormFavoriteAddressDAO) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	return dao.NewUserFavoriteAddressDAO(d.db).CountByUserID(ctx, userID)
}

func (d *gormFavoriteAddressDAO) GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error) {
	addr, err := dao.NewUserFavoriteAddressDAO(d.db).GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return addr, nil
}

func (d *gormFavoriteAddressDAO) Create(ctx context.Context, addr *model.UserFavoriteAddress) error {
	return dao.NewUserFavoriteAddressDAO(d.db).Create(ctx, addr)
}

func (d *gormFavoriteAddressDAO) Update(ctx context.Context, addr *model.UserFavoriteAddress) error {
	return dao.NewUserFavoriteAddressDAO(d.db).Update(ctx, addr)
}

func (d *gormFavoriteAddressDAO) Delete(ctx context.Context, id uint) error {
	return dao.NewUserFavoriteAddressDAO(d.db).Delete(ctx, id)
}

func (d *gormFavoriteAddressDAO) ClearDefaultByUserID(ctx context.Context, userID uint) error {
	return dao.NewUserFavoriteAddressDAO(d.db).ClearDefaultByUserID(ctx, userID)
}

func (d *gormFavoriteAddressDAO) FindMinIDByUserID(ctx context.Context, userID uint) (uint, bool, error) {
	return dao.NewUserFavoriteAddressDAO(d.db).FindMinIDByUserID(ctx, userID)
}

func (d *gormFavoriteAddressDAO) SetDefaultByID(ctx context.Context, id uint, isDefault bool) error {
	return dao.NewUserFavoriteAddressDAO(d.db).SetDefaultByID(ctx, id, isDefault)
}

// --- in-memory DAO for unit tests ---

type memFavoriteAddressDAO struct {
	mu     sync.Mutex
	nextID uint
	byID   map[uint]*model.UserFavoriteAddress
}

func newMemFavoriteAddressDAO() *memFavoriteAddressDAO {
	return &memFavoriteAddressDAO{
		nextID: 1,
		byID:   make(map[uint]*model.UserFavoriteAddress),
	}
}

func (d *memFavoriteAddressDAO) InTx(ctx context.Context, fn func(tx favoriteAddressTx) error) error {
	_ = ctx
	d.mu.Lock()
	defer d.mu.Unlock()
	return fn(d)
}

func (d *memFavoriteAddressDAO) ListByUserID(ctx context.Context, userID uint) ([]model.UserFavoriteAddress, error) {
	_ = ctx
	var out []model.UserFavoriteAddress
	for _, v := range d.byID {
		if v.DeletedAt.Valid {
			continue
		}
		if v.UserID == userID {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		if a.IsDefault != b.IsDefault {
			return a.IsDefault
		}
		return a.ID < b.ID
	})
	return out, nil
}

func (d *memFavoriteAddressDAO) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	_ = ctx
	var cnt int64
	for _, v := range d.byID {
		if v.DeletedAt.Valid {
			continue
		}
		if v.UserID == userID {
			cnt++
		}
	}
	return cnt, nil
}

func (d *memFavoriteAddressDAO) GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error) {
	_ = ctx
	v := d.byID[id]
	if v == nil || v.DeletedAt.Valid {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (d *memFavoriteAddressDAO) Create(ctx context.Context, addr *model.UserFavoriteAddress) error {
	_ = ctx
	addr.ID = d.nextID
	d.nextID++
	cp := *addr
	d.byID[cp.ID] = &cp
	addr.ID = cp.ID
	return nil
}

func (d *memFavoriteAddressDAO) Update(ctx context.Context, addr *model.UserFavoriteAddress) error {
	_ = ctx
	v := d.byID[addr.ID]
	if v == nil || v.DeletedAt.Valid {
		return ErrNotFound
	}
	cp := *addr
	d.byID[cp.ID] = &cp
	return nil
}

func (d *memFavoriteAddressDAO) Delete(ctx context.Context, id uint) error {
	_ = ctx
	v := d.byID[id]
	if v == nil || v.DeletedAt.Valid {
		return ErrNotFound
	}
	v.DeletedAt.Valid = true
	return nil
}

func (d *memFavoriteAddressDAO) ClearDefaultByUserID(ctx context.Context, userID uint) error {
	_ = ctx
	for _, v := range d.byID {
		if v.DeletedAt.Valid {
			continue
		}
		if v.UserID == userID {
			v.IsDefault = false
		}
	}
	return nil
}

func (d *memFavoriteAddressDAO) FindMinIDByUserID(ctx context.Context, userID uint) (uint, bool, error) {
	_ = ctx
	var min uint
	for _, v := range d.byID {
		if v.DeletedAt.Valid {
			continue
		}
		if v.UserID != userID {
			continue
		}
		if min == 0 || v.ID < min {
			min = v.ID
		}
	}
	if min == 0 {
		return 0, false, nil
	}
	return min, true, nil
}

func (d *memFavoriteAddressDAO) SetDefaultByID(ctx context.Context, id uint, isDefault bool) error {
	_ = ctx
	v := d.byID[id]
	if v == nil || v.DeletedAt.Valid {
		return ErrNotFound
	}
	v.IsDefault = isDefault
	return nil
}
