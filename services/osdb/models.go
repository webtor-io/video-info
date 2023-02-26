package osdb

import "time"

type SubtitleDownloadRequest struct {
	FileID    int    `json:"file_id"`
	SubFormat string `json:"sub_format,omitempty"`
}

type SubtitleDownloadResponse struct {
	Link         string    `json:"link"`
	FileName     string    `json:"file_name"`
	Requests     int       `json:"requests"`
	Remaining    int       `json:"remaining"`
	Message      string    `json:"message"`
	ResetTime    string    `json:"reset_time"`
	ResetTimeUtc time.Time `json:"reset_time_utc"`
}

type SubtitleSearchResponse struct {
	TotalPages int        `json:"total_pages"`
	TotalCount int        `json:"total_count"`
	Page       int        `json:"page"`
	Data       []Subtitle `json:"data"`
}

type Subtitle struct {
	Id         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		SubtitleId        string    `json:"subtitle_id"`
		Language          string    `json:"language"`
		DownloadCount     int       `json:"download_count"`
		NewDownloadCount  int       `json:"new_download_count"`
		HearingImpaired   bool      `json:"hearing_impaired"`
		Hd                bool      `json:"hd"`
		Fps               float64   `json:"fps"`
		Votes             int       `json:"votes"`
		Points            int       `json:"points"`
		Ratings           float64   `json:"ratings"`
		FromTrusted       bool      `json:"from_trusted"`
		ForeignPartsOnly  bool      `json:"foreign_parts_only"`
		AiTranslated      bool      `json:"ai_translated"`
		MachineTranslated bool      `json:"machine_translated"`
		UploadDate        time.Time `json:"upload_date"`
		Release           string    `json:"release"`
		Comments          string    `json:"comments"`
		LegacySubtitleId  int       `json:"legacy_subtitle_id"`
		Uploader          struct {
			UploaderId int    `json:"uploader_id"`
			Name       string `json:"name"`
			Rank       string `json:"rank"`
		} `json:"uploader"`
		FeatureDetails struct {
			FeatureId   int    `json:"feature_id"`
			FeatureType string `json:"feature_type"`
			Year        int    `json:"year"`
			Title       string `json:"title"`
			MovieName   string `json:"movie_name"`
			ImdbId      int    `json:"imdb_id"`
			TmdbId      int    `json:"tmdb_id"`
		} `json:"feature_details"`
		Url          string `json:"url"`
		RelatedLinks []struct {
			Label  string `json:"label"`
			Url    string `json:"url"`
			ImgUrl string `json:"img_url"`
		} `json:"related_links"`
		Files []struct {
			FileId   int    `json:"file_id"`
			CdNumber int    `json:"cd_number"`
			FileName string `json:"file_name"`
		} `json:"files"`
	} `json:"attributes"`
}
