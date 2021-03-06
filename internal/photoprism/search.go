package photoprism

import (
	"fmt"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/photoprism/photoprism/internal/forms"
	"github.com/photoprism/photoprism/internal/models"
	"github.com/photoprism/photoprism/internal/util"
)

// About 1km ('good enough' for now)
const SearchRadius = 0.009

// Search searches given an originals path and a db instance.
type Search struct {
	originalsPath string
	db            *gorm.DB
}

// SearchCount is the total number of search hits.
type SearchCount struct {
	Total int
}

// PhotoSearchResult is a found mediafile.
type PhotoSearchResult struct {
	// Photo
	ID                 uint
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          time.Time
	TakenAt            time.Time
	PhotoTitle         string
	PhotoDescription   string
	PhotoArtist        string
	PhotoKeywords      string
	PhotoColors        string
	PhotoColor         string
	PhotoCanonicalName string
	PhotoLat           float64
	PhotoLong          float64
	PhotoFavorite      bool

	// Camera
	CameraID    uint
	CameraModel string
	CameraMake  string

	// Lens
	LensID    uint
	LensModel string
	LensMake  string

	// Country
	CountryID   string
	CountryName string

	// Location
	LocationID     uint
	LocDisplayName string
	LocName        string
	LocCity        string
	LocPostcode    string
	LocCounty      string
	LocState       string
	LocCountry     string
	LocCountryCode string
	LocCategory    string
	LocType        string

	// File
	FileID             uint
	FilePrimary        bool
	FileMissing        bool
	FileName           string
	FileHash           string
	FilePerceptualHash string
	FileType           string
	FileMime           string
	FileWidth          int
	FileHeight         int
	FileOrientation    int
	FileAspectRatio    float64

	// Tags
	Tags string
}

// NewSearch returns a new Search type with a given path and db instance.
func NewSearch(originalsPath string, db *gorm.DB) *Search {
	instance := &Search{
		originalsPath: originalsPath,
		db:            db,
	}

	return instance
}

// Photos searches for photos based on a Form and returns a PhotoSearchResult slice.
func (s *Search) Photos(form forms.PhotoSearchForm) (results []PhotoSearchResult, err error) {
	if err := form.ParseQueryString(); err != nil {
		return results, err
	}

	defer util.ProfileTime(time.Now(), fmt.Sprintf("search for %+v", form))

	q := s.db.NewScope(nil).DB()

	// q.LogMode(true)

	q = q.Table("photos").
		Select(`SQL_CALC_FOUND_ROWS photos.*,
		files.id AS file_id, files.file_primary, files.file_missing, files.file_name, files.file_hash, 
		files.file_type, files.file_mime, files.file_width, files.file_height, files.file_aspect_ratio, 
		files.file_orientation, files.file_main_color, files.file_colors, files.file_luminance, files.file_chroma,
		cameras.camera_make, cameras.camera_model,
		lenses.lens_make, lenses.lens_model,
		countries.country_name,
		locations.loc_display_name, locations.loc_name, locations.loc_city, locations.loc_postcode, locations.loc_county, 
		locations.loc_state, locations.loc_country, locations.loc_country_code, locations.loc_category, locations.loc_type,
		GROUP_CONCAT(tags.tag_label) AS tags`).
		Joins("JOIN files ON files.photo_id = photos.id AND files.file_primary AND files.deleted_at IS NULL").
		Joins("JOIN cameras ON cameras.id = photos.camera_id").
		Joins("JOIN lenses ON lenses.id = photos.lens_id").
		Joins("LEFT JOIN countries ON countries.id = photos.country_id").
		Joins("LEFT JOIN locations ON locations.id = photos.location_id").
		Joins("LEFT JOIN photo_tags ON photo_tags.photo_id = photos.id").
		Joins("LEFT JOIN tags ON photo_tags.tag_id = tags.id").
		Where("photos.deleted_at IS NULL AND files.file_missing = 0").
		Group("photos.id, files.id")

	if form.Location == true {
		q = q.Where("location_id > 0")

		if form.Query != "" {
			likeString := "%" + strings.ToLower(form.Query) + "%"
			q = q.Where("LOWER(locations.loc_display_name) LIKE ?", likeString)
		}
	} else if form.Query != "" {
		likeString := "%" + strings.ToLower(form.Query) + "%"
		q = q.Where("tags.tag_label LIKE ? OR LOWER(photo_title) LIKE ? OR LOWER(files.file_main_color) LIKE ?", likeString, likeString, likeString)
	}

	if form.Camera > 0 {
		q = q.Where("photos.camera_id = ?", form.Camera)
	}

	if form.Color != "" {
		q = q.Where("files.file_main_color = ?", form.Color)
	}

	if form.Favorites {
		q = q.Where("photos.photo_favorite = 1")
	}

	if form.Country != "" {
		q = q.Where("locations.loc_country_code = ?", form.Country)
	}

	if form.Tags != "" {
		q = q.Where("tags.tag_label = ?", form.Tags)
	}

	if form.Title != "" {
		q = q.Where("LOWER(photos.photo_title) LIKE ?", fmt.Sprintf("%%%s%%", strings.ToLower(form.Title)))
	}

	if form.Description != "" {
		q = q.Where("LOWER(photos.photo_description) LIKE ?", fmt.Sprintf("%%%s%%", strings.ToLower(form.Description)))
	}

	if form.Notes != "" {
		q = q.Where("LOWER(photos.photo_notes) LIKE ?", fmt.Sprintf("%%%s%%", strings.ToLower(form.Notes)))
	}

	if form.Hash != "" {
		q = q.Where("files.file_hash = ?", form.Hash)
	}

	if form.Duplicate {
		q = q.Where("files.file_duplicate = 1")
	}

	if form.Portrait {
		q = q.Where("files.file_portrait = 1")
	}

	if form.Mono {
		q = q.Where("files.file_chroma = 0")
	} else if form.Chroma > 0 {
		q = q.Where("files.file_chroma > ?", form.Chroma)
	}

	if form.Fmin > 0 {
		q = q.Where("photos.photo_aperture >= ?", form.Fmin)
	}

	if form.Fmax > 0 {
		q = q.Where("photos.photo_aperture <= ?", form.Fmax)
	}

	if form.Dist == 0 {
		form.Dist = 20
	} else if form.Dist > 1000 {
		form.Dist = 1000
	}

	// Inaccurate distance search, but probably 'good enough' for now
	if form.Lat > 0 {
		latMin := form.Lat - SearchRadius*float64(form.Dist)
		latMax := form.Lat + SearchRadius*float64(form.Dist)
		q = q.Where("photos.photo_lat BETWEEN ? AND ?", latMin, latMax)
	}

	if form.Long > 0 {
		longMin := form.Long - SearchRadius*float64(form.Dist)
		longMax := form.Long + SearchRadius*float64(form.Dist)
		q = q.Where("photos.photo_long BETWEEN ? AND ?", longMin, longMax)
	}

	if !form.Before.IsZero() {
		q = q.Where("photos.taken_at <= ?", form.Before.Format("2006-01-02"))
	}

	if !form.After.IsZero() {
		q = q.Where("photos.taken_at >= ?", form.After.Format("2006-01-02"))
	}

	switch form.Order {
	case "newest":
		q = q.Order("taken_at DESC")
	case "oldest":
		q = q.Order("taken_at")
	case "imported":
		q = q.Order("created_at DESC")
	default:
		q = q.Order("taken_at DESC")
	}

	if form.Count > 0 && form.Count <= 1000 {
		q = q.Limit(form.Count).Offset(form.Offset)
	} else {
		q = q.Limit(100).Offset(0)
	}

	if result := q.Scan(&results); result.Error != nil {
		return results, result.Error
	}

	return results, nil
}

// FindFiles finds files returning maximum results defined by limit
// and finding them from an offest defined by offset.
func (s *Search) FindFiles(limit int, offset int) (files []models.File, err error) {
	if err := s.db.Where(&models.File{}).Limit(limit).Offset(offset).Find(&files).Error; err != nil {
		return files, err
	}

	return files, nil
}

// FindFileByID returns a mediafile given a certain ID.
func (s *Search) FindFileByID(id string) (file models.File, err error) {
	if err := s.db.Where("id = ?", id).Preload("Photo").First(&file).Error; err != nil {
		return file, err
	}

	return file, nil
}

// FindFileByHash finds a file with a given hash string.
func (s *Search) FindFileByHash(fileHash string) (file models.File, err error) {
	if err := s.db.Where("file_hash = ?", fileHash).Preload("Photo").First(&file).Error; err != nil {
		return file, err
	}

	return file, nil
}

// FindPhotoByID returns a Photo based on the ID.
func (s *Search) FindPhotoByID(photoID uint64) (photo models.Photo, err error) {
	if err := s.db.Where("id = ?", photoID).First(&photo).Error; err != nil {
		return photo, err
	}

	return photo, nil
}
