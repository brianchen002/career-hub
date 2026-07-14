package main

import (
	"testing"
	"time"
)

func TestBuildInsightStats(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.Local)
	data := dataFile{
		Applications: []application{
			{Status: "Prospect", Date: "2026-07-13"},
			{Status: "Applied", AppliedDate: "2026-07-13"},
			{Status: "Interview", Date: "2026-07-10"},
			{Status: "Offer", Date: "2026-06-11"},
			{Status: "Rejected", Date: "not-a-date"},
			{Status: "Archived", Date: "2026-07-01"},
		},
		Contacts: []contact{
			{Status: "To Reach Out"},
			{Status: "Sent"},
			{Status: "Replied", NextFollowup: "7-12-2026"},
			{Status: "Meeting", NextFollowup: "2026-07-20"},
			{Status: "Archived"},
		},
	}

	stats := buildInsightStats(data, now)
	if stats.Tracked != 5 || stats.Submitted != 4 || stats.Prospects != 1 {
		t.Fatalf("unexpected application counts: %#v", stats)
	}
	if stats.Applied != 1 || stats.Interviews != 1 || stats.Offers != 1 || stats.Rejected != 1 {
		t.Fatalf("unexpected pipeline breakdown: %#v", stats)
	}
	if stats.Undated != 1 || latestBucketCount(stats.Weekly) != 1 || latestBucketCount(stats.Monthly) != 2 {
		t.Fatalf("unexpected trend counts: %#v", stats)
	}
	if stats.Contacts != 4 || stats.Outreach != 3 || stats.Replies != 1 || stats.Meetings != 1 || stats.FollowupDue != 1 {
		t.Fatalf("unexpected networking counts: %#v", stats)
	}
}

func TestParseTrackerDateSupportsExistingFormats(t *testing.T) {
	for _, value := range []string{"2026-07-12", "7-12-2026", "07/12/2026"} {
		date, ok := parseTrackerDate(value)
		if !ok || date.Year() != 2026 || date.Month() != time.July || date.Day() != 12 {
			t.Fatalf("could not parse %q: %v %v", value, date, ok)
		}
	}
}

func TestStatusChangeCreatesTimelineEvent(t *testing.T) {
	path := t.TempDir() + "/career-hub.json"
	m := model{
		path:    path,
		section: applicationsSection,
		data: dataFile{
			NextApplicationID: 2,
			NextContactID:     1,
			NextActivityID:    1,
			Applications:      []application{{ID: 1, Company: "Example Co", Role: "Analyst", Status: "Prospect", Date: "2026-07-13"}},
		},
	}

	m.setStatus(1, "Applied")
	if m.data.Applications[0].AppliedDate == "" {
		t.Fatal("expected application date to be recorded")
	}
	if len(m.data.Activities) != 1 {
		t.Fatalf("expected one timeline event, got %d", len(m.data.Activities))
	}
	event := m.data.Activities[0]
	if event.EntityType != "Application" || event.Action != "Status changed" || event.Detail != "Prospect → Applied · application date recorded" {
		t.Fatalf("unexpected event: %#v", event)
	}

	reloaded, err := loadData(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Activities) != 1 || reloaded.NextActivityID != 2 {
		t.Fatalf("timeline was not persisted: %#v", reloaded)
	}
}

func TestTimelineFiltersActivities(t *testing.T) {
	m := model{section: timelineSection, data: dataFile{Activities: []activity{
		{ID: 1, EntityType: "Application", OccurredAt: "2026-07-12T10:00:00-07:00"},
		{ID: 2, EntityType: "Networking", OccurredAt: "2026-07-13T10:00:00-07:00"},
	}}}
	m.tab = 1 // Applications
	events := m.filteredActivities()
	if len(events) != 1 || events[0].EntityType != "Application" {
		t.Fatalf("unexpected filtered events: %#v", events)
	}
}

func TestStateFromLocation(t *testing.T) {
	tests := []struct {
		location string
		state    string
		remote   bool
	}{
		{"Seattle, WA", "WA", false},
		{"Austin, Texas", "TX", false},
		{"New York, NY", "NY", false},
		{"Washington, DC", "DC", false},
		{"Remote — United States", "", true},
		{"United States", "", false},
	}
	for _, test := range tests {
		state, remote := stateFromLocation(test.location)
		if state != test.state || remote != test.remote {
			t.Fatalf("%q: got state=%q remote=%v", test.location, state, remote)
		}
	}
}

func TestBuildGeographyStats(t *testing.T) {
	stats := buildGeographyStats(dataFile{Applications: []application{
		{Status: "Applied", Location: "Seattle, WA"},
		{Status: "Interview", Location: "Austin, Texas"},
		{Status: "Offer", Location: "Remote, US"},
		{Status: "Rejected", Location: "United States"},
		{Status: "Prospect", Location: "San Francisco, CA"},
		{Status: "Archived", Location: "New York, NY"},
	}})
	if stats.Submitted != 4 || stats.States["WA"] != 1 || stats.States["TX"] != 1 || stats.Remote != 1 || stats.Unknown != 1 {
		t.Fatalf("unexpected geography stats: %#v", stats)
	}
}

func TestBundledUSStateBordersRender(t *testing.T) {
	outlines, err := usStateOutlines()
	if err != nil {
		t.Fatal(err)
	}
	if len(outlines) < 51 || len(outlines["IL"].Lines) == 0 || len(outlines["AK"].Lines) == 0 {
		t.Fatalf("missing expected state borders: %d outlines", len(outlines))
	}

	mapView, err := renderUSOutlineMap(map[string]int{"IL": 2, "TX": 1}, 90)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapView) == 0 || !contains(mapView, "●") {
		t.Fatalf("expected a rendered map with state markers, got %q", mapView)
	}
}

func TestBuildMissionStatsUsesOnlyRecordedActions(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.Local)
	data := dataFile{
		Applications: []application{
			{Company: "One", Role: "Analyst", Status: "Applied", AppliedDate: "2026-07-14", Location: "Seattle, WA"},
			{Company: "Two", Role: "Analyst", Status: "Interview", AppliedDate: "2026-07-14", Location: "Austin, TX"},
			{Company: "Three", Role: "Analyst", Status: "Applied", AppliedDate: "2026-07-02", Location: "Chicago, IL"},
		},
		Contacts: []contact{{Status: "Sent"}, {Status: "Replied"}},
		Activities: []activity{
			{EntityType: "Application", OccurredAt: "2026-07-13T10:00:00-07:00"},
			{EntityType: "Networking", OccurredAt: "2026-07-13T12:00:00-07:00"},
			{EntityType: "Networking", OccurredAt: "2026-07-14T09:00:00-07:00"},
		},
	}

	stats := buildMissionStats(data, now)
	if stats.TodayActions != 1 || stats.WeekActions != 3 || stats.WeekApps != 2 || stats.WeekNetworking != 2 {
		t.Fatalf("unexpected mission counts: %#v", stats)
	}
	if stats.CurrentStreak != 2 || stats.BestStreak != 2 || len(stats.Heatmap) != 28 {
		t.Fatalf("unexpected streak or heatmap: %#v", stats)
	}
	if stats.Level < 1 || len(stats.Quests) != 3 || len(stats.Achievements) != 7 || len(stats.Journeys) != 3 {
		t.Fatalf("unexpected mission presentation data: %#v", stats)
	}
}

func TestWeeklyGoalsPersistAndDriveQuestTargets(t *testing.T) {
	path := t.TempDir() + "/career-hub.json"
	m := model{
		path:  path,
		data:  dataFile{Goals: resolvedWeeklyGoals(weeklyGoals{})},
		goals: formState{values: []string{"8", "4", "10"}},
	}
	updated, _ := m.saveGoalsForm()
	saved := updated.(model)
	if saved.view != listScreen || saved.data.Goals != (weeklyGoals{Applications: 8, Networking: 4, Actions: 10}) {
		t.Fatalf("goals were not saved to model: %#v", saved)
	}
	reloaded, err := loadData(path)
	if err != nil {
		t.Fatal(err)
	}
	stats := buildMissionStats(reloaded, time.Date(2026, time.July, 13, 12, 0, 0, 0, time.Local))
	if stats.Quests[0].Target != 8 || stats.Quests[1].Target != 4 || stats.Quests[2].Target != 10 {
		t.Fatalf("goals do not drive quest targets: %#v", stats.Quests)
	}
}

func TestSourceAttributionGroupsActiveApplications(t *testing.T) {
	sources := buildSourceStats(dataFile{Applications: []application{
		{Status: "Applied", Source: "LinkedIn"},
		{Status: "Rejected", Source: " LinkedIn  "},
		{Status: "Interview", Source: "Company site"},
		{Status: "Offer", Source: "Referral"},
		{Status: "Prospect", Source: "Recruiter"},
		{Status: "Archived", Source: "LinkedIn"},
		{Status: "Applied"},
	}})
	if len(sources) != 5 {
		t.Fatalf("expected five visible sources, got %#v", sources)
	}
	if sources[0].Source != "LinkedIn" || sources[0].Submitted != 2 || sources[0].Advanced != 0 {
		t.Fatalf("unexpected leading source: %#v", sources[0])
	}
	byName := make(map[string]sourceStat)
	for _, source := range sources {
		byName[source.Source] = source
	}
	if byName["Company site"].Advanced != 1 || byName["Referral"].Offers != 1 || byName["Unspecified"].Submitted != 1 {
		t.Fatalf("unexpected source outcomes: %#v", byName)
	}
}

func TestStatusChangesQueueOnlyRealMilestoneEggs(t *testing.T) {
	path := t.TempDir() + "/career-hub.json"
	m := model{
		path:    path,
		section: applicationsSection,
		data: dataFile{
			NextApplicationID: 2,
			NextContactID:     1,
			NextActivityID:    1,
			Goals:             weeklyGoals{Applications: 1, Networking: 3, Actions: 5},
			Applications:      []application{{ID: 1, Company: "Example Co", Role: "Analyst", Location: "Seattle, WA", Status: "Prospect"}},
		},
	}

	m.setStatus(1, "Applied")
	if len(m.eggQueue) != 2 || m.eggQueue[0].Kind != "new-state" || m.eggQueue[1].Kind != "pipeline" {
		t.Fatalf("expected state and pipeline milestones, got %#v", m.eggQueue)
	}
	if m.data.EasterEggs.LastPipelineWeek == "" {
		t.Fatal("pipeline milestone must persist the completed week")
	}
	if !m.showNextEgg() || m.view != eggScreen || m.egg.Kind != "new-state" {
		t.Fatalf("queued milestone did not become a visible signal: %#v", m)
	}
}

func TestThemeUnlocksFollowRecordedMilestones(t *testing.T) {
	data := dataFile{
		Applications: []application{
			{Status: "Applied"}, {Status: "Applied"}, {Status: "Interview"}, {Status: "Rejected"}, {Status: "Offer"},
		},
		Contacts: []contact{{Status: "Replied"}},
	}
	events := unlockEligibleThemes(&data)
	if !data.EasterEggs.UnlockedThemes["ocean"] || !data.EasterEggs.UnlockedThemes["amber"] || !data.EasterEggs.UnlockedThemes["phosphor"] || !data.EasterEggs.UnlockedThemes["ruby"] {
		t.Fatalf("expected all milestone themes to unlock: %#v", data.EasterEggs)
	}
	if len(events) != 3 {
		t.Fatalf("expected three new theme notifications, got %#v", events)
	}
	if repeat := unlockEligibleThemes(&data); len(repeat) != 0 {
		t.Fatalf("unlocked themes should not repeatedly announce themselves: %#v", repeat)
	}
}

func TestBootViewUsesSteamworksTerminalIdentity(t *testing.T) {
	view := model{view: bootScreen, bootPhase: 5}.viewBoot()
	if !contains(view, "STEAMWORKS 1984") || !contains(view, "PERSONAL OPPORTUNITY ENGINE") {
		t.Fatalf("boot screen lost its retro terminal identity: %q", view)
	}
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
