package photos

import "errors"

const (
	MaxNewPhotos    = 10
	MaxPhotoBytes   = 20 << 20
	MaxRequestBytes = 101 << 20
)

var (
	ErrNotFound    = errors.New("photos: photo not found")
	ErrUnsupported = errors.New("photos: unsupported image format")
	ErrEmpty       = errors.New("photos: empty image")
	ErrTooLarge    = errors.New("photos: image is too large")
	ErrTooMany     = errors.New("photos: too many images")
	ErrUnsafePath  = errors.New("photos: unsafe stored path")
	ErrInvalidDate = errors.New("photos: invalid entry date")
)

type Photo struct {
	ID               int64
	DayID            int64
	RelativePath     string
	OriginalFilename string
	MIMEType         string
	FileSize         int64
	CreatedAt        string
}

type Staged struct {
	TemporaryPath    string
	RelativePath     string
	OriginalFilename string
	MIMEType         string
	FileSize         int64
}
