package api

type Track struct {
	ID       string
	Title    string
	Artist   string
	Album    string
	AlbumID  string // album browse id (MPREb…) when the source row linked one
	Duration string
	Year     string
}

// AlbumRef is a lightweight pointer to an album (used in the artist view). ID is
// the album browse id (MPREb…) that AlbumByID can expand into a track list.
type AlbumRef struct {
	ID     string
	Title  string
	Artist string
	Year   string
}

// ArtistResult is an artist's landing page: top songs plus their albums/singles.
type ArtistResult struct {
	Name   string
	Songs  []Track
	Albums []AlbumRef
}
