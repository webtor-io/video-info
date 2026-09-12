package services

import (
	"sort"

	"github.com/webtor-io/video-info/services/osdb"
)

// RankSubtitles drops tracks that are useless as a full-dialogue
// source (forced-only, machine/AI translated), orders the rest so a
// hash-matched (already in sync) track wins, then trusted uploaders,
// then popularity, and keeps at most perLang per language. Languages
// keep their first-seen order so the caller's listing stays stable.
func RankSubtitles(subs []osdb.Subtitle, perLang int) []osdb.Subtitle {
	byLang := map[string][]osdb.Subtitle{}
	var order []string
	for _, s := range subs {
		a := s.Attributes
		if a.ForeignPartsOnly || a.AiTranslated || a.MachineTranslated {
			continue
		}
		if _, ok := byLang[a.Language]; !ok {
			order = append(order, a.Language)
		}
		byLang[a.Language] = append(byLang[a.Language], s)
	}
	var res []osdb.Subtitle
	for _, lang := range order {
		group := byLang[lang]
		sort.SliceStable(group, func(i, j int) bool {
			ai, aj := group[i].Attributes, group[j].Attributes
			if ai.MoviehashMatch != aj.MoviehashMatch {
				return ai.MoviehashMatch
			}
			if ai.FromTrusted != aj.FromTrusted {
				return ai.FromTrusted
			}
			return ai.DownloadCount > aj.DownloadCount
		})
		if perLang > 0 && len(group) > perLang {
			group = group[:perLang]
		}
		res = append(res, group...)
	}
	return res
}
