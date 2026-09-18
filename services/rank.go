package services

import (
	"sort"

	"github.com/webtor-io/video-info/services/osdb"
)

// RankSubtitles drops tracks that are useless as a full-dialogue
// source (forced-only, machine/AI translated), orders the rest so a
// hash-matched (already in sync) track wins, then trusted uploaders,
// then popularity, and keeps at most perLang per language; perLang <= 0
// keeps every surviving track. Languages keep their first-seen order so
// the caller's listing stays stable.
func RankSubtitles(subs []osdb.Subtitle, perLang int) []osdb.Subtitle {
	ranked, _ := RankSubtitlesDropped(subs, perLang)
	return ranked
}

// RankSubtitlesDropped is RankSubtitles that also names the languages the
// filter removed ENTIRELY — every candidate was machine/AI translated or
// forced-only, so the viewer loses the language rather than a bad track.
// It exists to be counted: whether "MT-only languages lose their only
// track" is worth a policy is an open question, and the answer is a Loki
// query over these, not an opinion.
func RankSubtitlesDropped(subs []osdb.Subtitle, perLang int) ([]osdb.Subtitle, []string) {
	byLang := map[string][]osdb.Subtitle{}
	seen := map[string]bool{}
	var seenOrder []string
	var order []string
	for _, s := range subs {
		a := s.Attributes
		if !seen[a.Language] {
			seen[a.Language] = true
			seenOrder = append(seenOrder, a.Language)
		}
		if a.ForeignPartsOnly || a.AiTranslated || a.MachineTranslated {
			continue
		}
		if _, ok := byLang[a.Language]; !ok {
			order = append(order, a.Language)
		}
		byLang[a.Language] = append(byLang[a.Language], s)
	}
	var dropped []string
	for _, lang := range seenOrder {
		if _, kept := byLang[lang]; !kept {
			dropped = append(dropped, lang)
		}
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
	return res, dropped
}
