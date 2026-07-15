package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var appStatuses = []string{"Prospect", "Applied", "Interview", "Offer", "Rejected", "Archived"}
var contactStatuses = []string{"To Reach Out", "Sent", "Replied", "Meeting", "Nurture", "Archived"}

var stateNames = map[string]string{
	"AL": "Alabama", "AK": "Alaska", "AZ": "Arizona", "AR": "Arkansas", "CA": "California",
	"CO": "Colorado", "CT": "Connecticut", "DE": "Delaware", "DC": "District of Columbia", "FL": "Florida",
	"GA": "Georgia", "HI": "Hawaii", "ID": "Idaho", "IL": "Illinois", "IN": "Indiana",
	"IA": "Iowa", "KS": "Kansas", "KY": "Kentucky", "LA": "Louisiana", "ME": "Maine",
	"MD": "Maryland", "MA": "Massachusetts", "MI": "Michigan", "MN": "Minnesota", "MS": "Mississippi",
	"MO": "Missouri", "MT": "Montana", "NE": "Nebraska", "NV": "Nevada", "NH": "New Hampshire",
	"NJ": "New Jersey", "NM": "New Mexico", "NY": "New York", "NC": "North Carolina", "ND": "North Dakota",
	"OH": "Ohio", "OK": "Oklahoma", "OR": "Oregon", "PA": "Pennsylvania", "RI": "Rhode Island",
	"SC": "South Carolina", "SD": "South Dakota", "TN": "Tennessee", "TX": "Texas", "UT": "Utah",
	"VT": "Vermont", "VA": "Virginia", "WA": "Washington", "WV": "West Virginia", "WI": "Wisconsin", "WY": "Wyoming",
}

//go:embed assets/us-states-10m.json
var usStatesTopoJSON []byte

var (
	outlineOnce sync.Once
	outlineData map[string]stateOutline
	outlineErr  error
)

type application struct {
	ID          int    `json:"id"`
	Company     string `json:"company"`
	Role        string `json:"role"`
	Location    string `json:"location"`
	URL         string `json:"url"`
	Source      string `json:"source,omitempty"`
	Date        string `json:"date"`
	AppliedDate string `json:"applied_date,omitempty"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
}

type contact struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Company      string `json:"company"`
	Title        string `json:"title"`
	Relationship string `json:"relationship"`
	ProfileURL   string `json:"profile_url"`
	LastContact  string `json:"last_contact"`
	NextFollowup string `json:"next_followup"`
	Status       string `json:"status"`
	Notes        string `json:"notes"`
}

// task is a deliberately lightweight personal note. It has no external
// integration, so it can be used for any job-search reminder or freeform note.
type task struct {
	ID        int    `json:"id"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type activity struct {
	ID         int    `json:"id"`
	EntityType string `json:"entity_type"`
	EntityID   int    `json:"entity_id"`
	Subject    string `json:"subject"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	OccurredAt string `json:"occurred_at"`
}

type dataFile struct {
	NextApplicationID int           `json:"next_application_id"`
	NextContactID     int           `json:"next_contact_id"`
	NextTaskID        int           `json:"next_task_id"`
	NextActivityID    int           `json:"next_activity_id"`
	Goals             weeklyGoals   `json:"goals"`
	EasterEggs        easterEggs    `json:"easter_eggs"`
	Applications      []application `json:"applications"`
	Contacts          []contact     `json:"contacts"`
	Tasks             []task        `json:"tasks"`
	Activities        []activity    `json:"activities"`
}

type easterEggs struct {
	Theme             string          `json:"theme"`
	UnlockedThemes    map[string]bool `json:"unlocked_themes"`
	LastPipelineWeek  string          `json:"last_pipeline_week,omitempty"`
	OfferProtocolSeen bool            `json:"offer_protocol_seen,omitempty"`
}

type eggEvent struct {
	Kind   string
	Title  string
	Detail string
}

type weeklyGoals struct {
	Applications int `json:"applications"`
	Networking   int `json:"networking"`
	Actions      int `json:"actions"`
}

func resolvedWeeklyGoals(goals weeklyGoals) weeklyGoals {
	if goals.Applications < 1 {
		goals.Applications = 5
	}
	if goals.Networking < 1 {
		goals.Networking = 3
	}
	if goals.Actions < 1 {
		goals.Actions = 5
	}
	return goals
}

type section int

const (
	applicationsSection section = iota
	networkingSection
	insightsSection
	timelineSection
	geographySection
	missionSection
	tasksSection
)

type screen int

const (
	listScreen screen = iota
	formScreen
	statusScreen
	deleteScreen
	goalsScreen
	bootScreen
	eggScreen
	themeScreen
)

type formState struct {
	editing bool
	id      int
	field   int
	values  []string
}

type chartBucket struct {
	Label string
	Count int
	Start time.Time
}

type insightStats struct {
	Tracked     int
	Submitted   int
	Prospects   int
	Applied     int
	Interviews  int
	Offers      int
	Rejected    int
	Undated     int
	Weekly      []chartBucket
	Monthly     []chartBucket
	Contacts    int
	Outreach    int
	Replies     int
	Meetings    int
	FollowupDue int
}

type sourceStat struct {
	Source    string
	Tracked   int
	Submitted int
	Advanced  int
	Offers    int
}

// missionStats is derived from real saved records and audit events. The
// motivational layer never writes pretend activity into the tracker.
type missionStats struct {
	TodayActions   int
	WeekActions    int
	WeekApps       int
	WeekNetworking int
	CurrentStreak  int
	BestStreak     int
	XP             int
	Level          int
	FollowupDue    int
	Quests         []quest
	Achievements   []achievement
	Heatmap        []heatmapDay
	Journeys       []application
}

type quest struct {
	Name     string
	Detail   string
	Progress int
	Target   int
}

type achievement struct {
	Name     string
	Detail   string
	Progress int
	Target   int
}

type heatmapDay struct {
	Date  time.Time
	Count int
}

type geographyStats struct {
	Submitted int
	States    map[string]int
	Remote    int
	Unknown   int
}

type stateCount struct {
	Code  string
	Name  string
	Count int
}

type geoPoint struct {
	Lon float64
	Lat float64
}

type stateOutline struct {
	Code   string
	Lines  [][]geoPoint
	Center geoPoint
}

type topoTopology struct {
	Transform topoTransform         `json:"transform"`
	Objects   map[string]topoObject `json:"objects"`
	Arcs      [][][]int             `json:"arcs"`
}

type topoTransform struct {
	Scale     [2]float64 `json:"scale"`
	Translate [2]float64 `json:"translate"`
}

type topoObject struct {
	Geometries []topoGeometry `json:"geometries"`
}

type topoGeometry struct {
	Arcs       json.RawMessage `json:"arcs"`
	Properties struct {
		Name string `json:"name"`
	} `json:"properties"`
}

type brailleCanvas struct {
	Width   int
	Height  int
	Dots    [][]bool
	Markers map[[2]int]bool
}

type model struct {
	data         dataFile
	path         string
	section      section
	tab          int
	cursor       int
	view         screen
	form         formState
	goals        formState
	themeCursor  int
	bootPhase    int
	egg          eggEvent
	eggQueue     []eggEvent
	statusCursor int
	deleteID     int
	message      string
	width        int
	height       int
}

type statusMsg string
type errMsg error

var (
	titleStyle       lipgloss.Style
	brandStyle       lipgloss.Style
	systemStyle      lipgloss.Style
	mutedStyle       lipgloss.Style
	tableHeaderStyle lipgloss.Style
	keyStyle         lipgloss.Style
	goodStyle        lipgloss.Style
	warnStyle        lipgloss.Style
	badStyle         lipgloss.Style
	selectStyle      lipgloss.Style
	inputStyle       lipgloss.Style
	frameStyle       lipgloss.Style
	footerStyle      lipgloss.Style
)

func init() { applyTheme("ocean") }

func applyTheme(theme string) {
	accent, focus, system, muted, amber, good, bad, selectBG, footerBG, input := "#65F6E7", "#0E5A8A", "#8FA8B5", "#7893A1", "#FFD166", "#A8FF60", "#FF7085", "#0A4C5A", "#071A23", "#EAF7FF"
	switch theme {
	case "amber":
		accent, focus, system, muted, amber, good, bad, selectBG, footerBG, input = "#FFB547", "#8B3E18", "#C49A6C", "#8E7357", "#FFE0A3", "#D7FF79", "#FF6D4A", "#5B2B14", "#1C100A", "#FFF2D5"
	case "phosphor":
		accent, focus, system, muted, amber, good, bad, selectBG, footerBG, input = "#6AFF91", "#0A5125", "#A4CC9F", "#6A966F", "#F6F17A", "#B4FF79", "#FF8E8E", "#113F20", "#07150B", "#E6FFE9"
	case "ruby":
		accent, focus, system, muted, amber, good, bad, selectBG, footerBG, input = "#FF6A58", "#741C30", "#D2A0A6", "#986873", "#FFD36A", "#B9FF8A", "#FF8399", "#4D1830", "#1B0A14", "#FFF0EF"
	}
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(input)).Background(lipgloss.Color(focus)).Bold(true).Padding(0, 1)
	brandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true)
	systemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(system))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(muted))
	tableHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(amber)).Bold(true)
	keyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true)
	goodStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(good)).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(amber)).Bold(true)
	badStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(bad)).Bold(true)
	selectStyle = lipgloss.NewStyle().Background(lipgloss.Color(selectBG)).Foreground(lipgloss.Color(input)).Bold(true)
	inputStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(input))
	frameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accent))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(system)).Background(lipgloss.Color(footerBG)).Padding(0, 1)
}

func main() {
	dataPath := flag.String("data", "career-hub.json", "path to local career tracker data")
	flag.Parse()

	path, err := filepath.Abs(*dataPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	d, err := loadData(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	unlockEligibleThemes(&d)
	applyTheme(activeTheme(d.EasterEggs.Theme))
	p := tea.NewProgram(model{data: d, path: path, width: 120, height: 34, view: bootScreen}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type bootTickMsg struct{}

func bootTick() tea.Cmd {
	return tea.Tick(830*time.Millisecond, func(time.Time) tea.Msg { return bootTickMsg{} })
}

func (m model) Init() tea.Cmd { return bootTick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case statusMsg:
		m.message = string(msg)
		return m, nil
	case errMsg:
		m.message = badStyle.Render("Error: " + msg.Error())
		return m, nil
	case bootTickMsg:
		if m.view != bootScreen {
			return m, nil
		}
		m.bootPhase++
		if m.bootPhase >= 6 {
			m.view = listScreen
			m.message = "Steamworks console ready. Your tracker is stored locally."
			return m, nil
		}
		return m, bootTick()
	case tea.KeyMsg:
		switch m.view {
		case bootScreen:
			return m.updateBoot(msg)
		case eggScreen:
			return m.updateEgg(msg)
		case themeScreen:
			return m.updateThemePicker(msg)
		case formScreen:
			return m.updateForm(msg)
		case goalsScreen:
			return m.updateGoalsForm(msg)
		case statusScreen:
			return m.updateStatusPicker(msg)
		case deleteScreen:
			return m.updateDeletePicker(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleIndices()
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "1":
		m.section, m.tab, m.cursor = applicationsSection, 0, 0
	case "2":
		m.section, m.tab, m.cursor = networkingSection, 0, 0
	case "3":
		m.section, m.tab, m.cursor = insightsSection, 0, 0
	case "4":
		m.section, m.tab, m.cursor = timelineSection, 0, 0
	case "5":
		m.section, m.tab, m.cursor = geographySection, 0, 0
	case "6":
		m.section, m.tab, m.cursor = missionSection, 0, 0
	case "7":
		m.section, m.tab, m.cursor = tasksSection, 0, 0
	case "left", "h":
		if m.section != geographySection && m.section != missionSection {
			m.tab = (m.tab - 1 + len(m.sectionTabs())) % len(m.sectionTabs())
			m.cursor = 0
		}
	case "right", "l":
		if m.section != geographySection && m.section != missionSection {
			m.tab = (m.tab + 1) % len(m.sectionTabs())
			m.cursor = 0
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case "a":
		if m.section != insightsSection && m.section != timelineSection && m.section != geographySection && m.section != missionSection {
			m.startNewForm()
		}
	case "e":
		if m.section != insightsSection && m.section != timelineSection && m.section != geographySection && m.section != missionSection {
			m.startEditForm()
		}
	case "g":
		if m.section == missionSection {
			m.startGoalsForm()
		}
	case "y":
		if m.section == missionSection {
			m.startThemePicker()
		}
	case "c":
		if m.hasSelection() {
			m.view = statusScreen
			m.statusCursor = indexOf(m.sectionStatuses(), m.selectedStatus())
			if m.statusCursor < 0 {
				m.statusCursor = 0
			}
		}
	case "x":
		if m.hasSelection() {
			if m.section == tasksSection {
				m.setStatus(m.selectedID(), "Done")
				m.message = "Marked selected note as done."
			} else {
				m.setStatus(m.selectedID(), "Archived")
				m.message = "Archived selected record."
			}
		}
	case "D":
		if m.hasSelection() {
			m.view, m.deleteID = deleteScreen, m.selectedID()
		}
	case "o":
		url := m.selectedURL()
		if url == "" {
			m.message = warnStyle.Render("No URL saved for this record.")
			return m, nil
		}
		return m, openURL(url)
	case "r":
		d, err := loadData(m.path)
		if err != nil {
			m.message = badStyle.Render("Reload failed: " + err.Error())
		} else {
			m.data, m.message = d, "Reloaded local data."
		}
	}
	return m, nil
}

func (m *model) startNewForm() {
	if m.section == insightsSection || m.section == timelineSection || m.section == geographySection || m.section == missionSection {
		return
	}
	m.view = formScreen
	if m.section == applicationsSection {
		m.form = formState{values: []string{"", "", "", "", "", time.Now().Format("2006-01-02"), ""}}
	} else if m.section == networkingSection {
		m.form = formState{values: []string{"", "", "", "", "", "", time.Now().Format("2006-01-02"), ""}}
	} else {
		m.form = formState{values: []string{""}}
	}
}

func (m *model) startGoalsForm() {
	goals := resolvedWeeklyGoals(m.data.Goals)
	m.view = goalsScreen
	m.goals = formState{values: []string{
		strconv.Itoa(goals.Applications),
		strconv.Itoa(goals.Networking),
		strconv.Itoa(goals.Actions),
	}}
}

type themeChoice struct {
	Key         string
	Name        string
	Description string
}

func (m *model) startThemePicker() {
	normalizeEasterEggs(&m.data)
	choices := availableThemes(m.data.EasterEggs)
	m.themeCursor = 0
	for index, choice := range choices {
		if choice.Key == activeTheme(m.data.EasterEggs.Theme) {
			m.themeCursor = index
			break
		}
	}
	m.view = themeScreen
}

func (m *model) startEditForm() {
	if m.section == applicationsSection {
		if j, ok := m.selectedApplication(); ok {
			m.view = formScreen
			m.form = formState{editing: true, id: j.ID, values: []string{j.Company, j.Role, j.Location, j.URL, j.Source, j.Date, j.Notes}}
		}
		return
	}
	if c, ok := m.selectedContact(); ok {
		m.view = formScreen
		m.form = formState{editing: true, id: c.ID, values: []string{c.Name, c.Company, c.Title, c.Relationship, c.ProfileURL, c.LastContact, c.NextFollowup, c.Notes}}
	}
	if t, ok := m.selectedTask(); ok {
		m.view = formScreen
		m.form = formState{editing: true, id: t.ID, values: []string{t.Text}}
	}
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view, m.message = listScreen, "Edit cancelled."
		return m, nil
	case "up", "shift+tab":
		if m.form.field > 0 {
			m.form.field--
		}
		return m, nil
	case "down", "tab", "enter":
		if m.form.field < len(m.form.values)-1 {
			m.form.field++
			return m, nil
		}
		return m.saveForm()
	case "ctrl+s":
		return m.saveForm()
	case "backspace":
		value := m.form.values[m.form.field]
		if len(value) > 0 {
			_, size := utf8.DecodeLastRuneInString(value)
			m.form.values[m.form.field] = value[:len(value)-size]
		}
		return m, nil
	}
	if len(msg.Runes) > 0 {
		m.form.values[m.form.field] += string(msg.Runes)
	}
	return m, nil
}

func (m model) updateGoalsForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view, m.message = listScreen, "Goal changes cancelled."
		return m, nil
	case "up", "shift+tab":
		if m.goals.field > 0 {
			m.goals.field--
		}
		return m, nil
	case "down", "tab", "enter":
		if m.goals.field < len(m.goals.values)-1 {
			m.goals.field++
			return m, nil
		}
		return m.saveGoalsForm()
	case "ctrl+s":
		return m.saveGoalsForm()
	case "backspace":
		value := m.goals.values[m.goals.field]
		if len(value) > 0 {
			_, size := utf8.DecodeLastRuneInString(value)
			m.goals.values[m.goals.field] = value[:len(value)-size]
		}
		return m, nil
	}
	if len(msg.Runes) > 0 {
		m.goals.values[m.goals.field] += string(msg.Runes)
	}
	return m, nil
}

func (m model) updateBoot(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	m.bootPhase = 6
	m.view = listScreen
	m.message = "Steamworks console ready. Your tracker is stored locally."
	return m, nil
}

func (m model) updateEgg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.showNextEgg() {
		return m, nil
	}
	m.view = listScreen
	return m, nil
}

func (m model) updateThemePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := availableThemes(m.data.EasterEggs)
	switch msg.String() {
	case "esc", "q":
		m.view, m.message = listScreen, "Theme selection cancelled."
	case "up", "k":
		if m.themeCursor > 0 {
			m.themeCursor--
		}
	case "down", "j":
		if m.themeCursor < len(choices)-1 {
			m.themeCursor++
		}
	case "enter":
		if len(choices) == 0 {
			m.view = listScreen
			return m, nil
		}
		choice := choices[m.themeCursor]
		m.data.EasterEggs.Theme = choice.Key
		applyTheme(choice.Key)
		if err := saveData(m.path, m.data); err != nil {
			m.message = badStyle.Render("Save failed: " + err.Error())
			return m, nil
		}
		m.view = listScreen
		m.message = choice.Name + " theme active."
	}
	return m, nil
}

func (m model) saveGoalsForm() (tea.Model, tea.Cmd) {
	labels := []string{"Applications", "Networking actions", "Tracker actions"}
	values := make([]int, len(labels))
	for index, raw := range m.goals.values {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 1 || value > 99 {
			m.message = badStyle.Render(labels[index] + " goal must be a whole number from 1 to 99.")
			return m, nil
		}
		values[index] = value
	}
	m.data.Goals = weeklyGoals{Applications: values[0], Networking: values[1], Actions: values[2]}
	if err := saveData(m.path, m.data); err != nil {
		m.message = badStyle.Render("Save failed: " + err.Error())
		return m, nil
	}
	m.view, m.message = listScreen, "Weekly goals saved."
	return m, nil
}

func (m model) saveForm() (tea.Model, tea.Cmd) {
	now := time.Now().Format("2006-01-02")
	if m.section == applicationsSection {
		company, role := strings.TrimSpace(m.form.values[0]), strings.TrimSpace(m.form.values[1])
		if company == "" || role == "" {
			m.message = badStyle.Render("Company and role are required.")
			return m, nil
		}
		j := application{Company: company, Role: role, Location: strings.TrimSpace(m.form.values[2]), URL: strings.TrimSpace(m.form.values[3]), Source: strings.TrimSpace(m.form.values[4]), Date: strings.TrimSpace(m.form.values[5]), Notes: strings.TrimSpace(m.form.values[6])}
		if j.Date == "" {
			j.Date = now
		}
		if m.form.editing {
			for i := range m.data.Applications {
				if m.data.Applications[i].ID == m.form.id {
					j.ID = m.form.id
					j.Status = m.data.Applications[i].Status
					j.AppliedDate = m.data.Applications[i].AppliedDate
					m.data.Applications[i] = j
					m.addActivity("Application", j.ID, applicationSubject(j), "Updated", "Edited application details")
					break
				}
			}
			m.message = "Updated " + company + " — " + role
		} else {
			j.ID, j.Status = m.data.NextApplicationID, "Prospect"
			m.data.NextApplicationID++
			m.data.Applications = append(m.data.Applications, j)
			m.addActivity("Application", j.ID, applicationSubject(j), "Created", "Added as Prospect")
			m.message = "Added " + company + " — " + role
		}
	} else if m.section == networkingSection {
		name := strings.TrimSpace(m.form.values[0])
		if name == "" {
			m.message = badStyle.Render("Contact name is required.")
			return m, nil
		}
		c := contact{Name: name, Company: strings.TrimSpace(m.form.values[1]), Title: strings.TrimSpace(m.form.values[2]), Relationship: strings.TrimSpace(m.form.values[3]), ProfileURL: strings.TrimSpace(m.form.values[4]), LastContact: strings.TrimSpace(m.form.values[5]), NextFollowup: strings.TrimSpace(m.form.values[6]), Notes: strings.TrimSpace(m.form.values[7])}
		if m.form.editing {
			for i := range m.data.Contacts {
				if m.data.Contacts[i].ID == m.form.id {
					c.ID, c.Status = m.form.id, m.data.Contacts[i].Status
					m.data.Contacts[i] = c
					m.addActivity("Networking", c.ID, contactSubject(c), "Updated", "Edited contact details")
					break
				}
			}
			m.message = "Updated " + name
		} else {
			c.ID, c.Status = m.data.NextContactID, "To Reach Out"
			m.data.NextContactID++
			m.data.Contacts = append(m.data.Contacts, c)
			m.addActivity("Networking", c.ID, contactSubject(c), "Created", "Added to outreach list")
			m.message = "Added " + name
		}
	} else {
		text := strings.TrimSpace(m.form.values[0])
		if text == "" {
			m.message = badStyle.Render("Task text is required.")
			return m, nil
		}
		nowTimestamp := time.Now().Format(time.RFC3339)
		if m.form.editing {
			for i := range m.data.Tasks {
				if m.data.Tasks[i].ID == m.form.id {
					m.data.Tasks[i].Text = text
					m.data.Tasks[i].UpdatedAt = nowTimestamp
					m.addActivity("Task", m.form.id, taskSubject(m.data.Tasks[i]), "Updated", "Edited task note")
					break
				}
			}
			m.message = "Updated task note."
		} else {
			t := task{ID: m.data.NextTaskID, Text: text, CreatedAt: nowTimestamp, UpdatedAt: nowTimestamp}
			m.data.NextTaskID++
			m.data.Tasks = append(m.data.Tasks, t)
			m.addActivity("Task", t.ID, taskSubject(t), "Created", "Added to task pad")
			m.message = "Added task note."
		}
	}
	if err := saveData(m.path, m.data); err != nil {
		m.message = badStyle.Render("Save failed: " + err.Error())
	}
	m.view, m.cursor = listScreen, 0
	return m, nil
}

func (m model) updateStatusPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	options := m.sectionStatuses()
	switch msg.String() {
	case "esc", "q":
		m.view = listScreen
	case "up", "k":
		if m.statusCursor > 0 {
			m.statusCursor--
		}
	case "down", "j":
		if m.statusCursor < len(options)-1 {
			m.statusCursor++
		}
	case "enter":
		m.setStatus(m.selectedID(), options[m.statusCursor])
		m.message = "Status changed to " + options[m.statusCursor]
		if !m.showNextEgg() {
			m.view = listScreen
		}
	}
	return m, nil
}

func (m model) updateDeletePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n", "q":
		m.view, m.message = listScreen, "Delete cancelled."
	case "y", "enter":
		entityType, subject := "Application", m.selectedLabel()
		if m.section == applicationsSection {
			m.data.Applications = removeApplication(m.data.Applications, m.deleteID)
		} else if m.section == networkingSection {
			entityType = "Networking"
			m.data.Contacts = removeContact(m.data.Contacts, m.deleteID)
		} else {
			entityType = "Task"
			m.data.Tasks = removeTask(m.data.Tasks, m.deleteID)
		}
		m.addActivity(entityType, m.deleteID, subject, "Deleted", "Record permanently removed from tracker")
		if err := saveData(m.path, m.data); err != nil {
			m.message = badStyle.Render("Delete failed: " + err.Error())
		} else {
			m.message = "Record permanently deleted."
		}
		m.view, m.cursor = listScreen, 0
	}
	return m, nil
}

func (m *model) setStatus(id int, status string) {
	normalizeEasterEggs(&m.data)
	if m.section == applicationsSection {
		beforeGeography := buildGeographyStats(m.data)
		hadOffer := hasApplicationStatus(m.data.Applications, "Offer")
		for i := range m.data.Applications {
			if m.data.Applications[i].ID == id {
				previous := m.data.Applications[i].Status
				if strings.EqualFold(previous, status) {
					break
				}
				wasSubmitted := isSubmitted(previous)
				m.data.Applications[i].Status = status
				detail := previous + " → " + status
				if isSubmitted(status) && !wasSubmitted && m.data.Applications[i].AppliedDate == "" {
					m.data.Applications[i].AppliedDate = time.Now().Format("2006-01-02")
					detail += " · application date recorded"
				}
				m.addActivity("Application", id, applicationSubject(m.data.Applications[i]), "Status changed", detail)
				if isSubmitted(status) && !wasSubmitted {
					if state, _ := stateFromLocation(m.data.Applications[i].Location); state != "" && beforeGeography.States[state] == 0 {
						m.queueEgg(eggEvent{Kind: "new-state", Title: "NEW TERRITORY SIGNAL", Detail: state + " // " + stateNames[state] + " is now on your submitted-application map."})
					}
					goals := resolvedWeeklyGoals(m.data.Goals)
					stats := buildMissionStats(m.data, time.Now())
					week := weekKey(time.Now())
					if stats.WeekApps >= goals.Applications && m.data.EasterEggs.LastPipelineWeek != week {
						m.data.EasterEggs.LastPipelineWeek = week
						m.queueEgg(eggEvent{Kind: "pipeline", Title: "PIPELINE TRANSMISSION", Detail: fmt.Sprintf("Weekly application target reached: %d / %d submitted.", stats.WeekApps, goals.Applications)})
					}
				}
				if strings.EqualFold(status, "Offer") && !hadOffer {
					m.data.EasterEggs.OfferProtocolSeen = true
					m.queueEgg(eggEvent{Kind: "offer", Title: "OFFER PROTOCOL", Detail: "A real offer is now recorded. Pause, verify the details, and celebrate this milestone."})
				}
				break
			}
		}
	} else if m.section == networkingSection {
		hadReply := hasContactStatus(m.data.Contacts, "Replied")
		for i := range m.data.Contacts {
			if m.data.Contacts[i].ID == id {
				previous := m.data.Contacts[i].Status
				if strings.EqualFold(previous, status) {
					break
				}
				m.data.Contacts[i].Status = status
				m.addActivity("Networking", id, contactSubject(m.data.Contacts[i]), "Status changed", previous+" → "+status)
				if strings.EqualFold(status, "Replied") && !hadReply {
					m.queueEgg(eggEvent{Kind: "reply", Title: "INBOUND SIGNAL DETECTED", Detail: "First recorded reply. A warm conversation has entered your network console."})
				}
				break
			}
		}
	} else {
		for i := range m.data.Tasks {
			if m.data.Tasks[i].ID == id {
				previous := taskStatus(m.data.Tasks[i])
				if strings.EqualFold(previous, status) {
					break
				}
				m.data.Tasks[i].Done = strings.EqualFold(status, "Done")
				m.data.Tasks[i].UpdatedAt = time.Now().Format(time.RFC3339)
				m.addActivity("Task", id, taskSubject(m.data.Tasks[i]), "Status changed", previous+" → "+taskStatus(m.data.Tasks[i]))
				break
			}
		}
	}
	if m.section == applicationsSection || m.section == networkingSection {
		for _, event := range unlockEligibleThemes(&m.data) {
			m.queueEgg(event)
		}
	}
	if err := saveData(m.path, m.data); err != nil {
		m.message = badStyle.Render("Save failed: " + err.Error())
	}
}

func (m *model) queueEgg(event eggEvent) {
	m.eggQueue = append(m.eggQueue, event)
}

func (m *model) showNextEgg() bool {
	if len(m.eggQueue) == 0 {
		return false
	}
	m.egg = m.eggQueue[0]
	m.eggQueue = m.eggQueue[1:]
	m.view = eggScreen
	return true
}

func hasApplicationStatus(items []application, status string) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, status) {
			return true
		}
	}
	return false
}

func hasContactStatus(items []contact, status string) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, status) {
			return true
		}
	}
	return false
}

func weekKey(date time.Time) string {
	return startOfWeek(date).Format("2006-01-02")
}

func normalizeEasterEggs(data *dataFile) {
	if data.EasterEggs.UnlockedThemes == nil {
		data.EasterEggs.UnlockedThemes = make(map[string]bool)
	}
	if activeTheme(data.EasterEggs.Theme) == "ocean" {
		data.EasterEggs.Theme = "ocean"
	}
	data.EasterEggs.UnlockedThemes["ocean"] = true
}

func activeTheme(theme string) string {
	switch theme {
	case "amber", "phosphor", "ruby":
		return theme
	default:
		return "ocean"
	}
}

func unlockEligibleThemes(data *dataFile) []eggEvent {
	normalizeEasterEggs(data)
	insights := buildInsightStats(*data, time.Now())
	checks := []struct {
		key, name, detail string
		unlocked          bool
	}{
		{"amber", "AMBER FOUNDRY", "Unlocked after five submitted applications. Copper, brass, and CRT amber are now available in Mission Control.", insights.Submitted >= 5},
		{"phosphor", "PHOSPHOR VECTOR", "Unlocked after your first recorded reply. Green-screen signal mode is now available in Mission Control.", insights.Replies >= 1},
		{"ruby", "RUBY SYNTHWAVE", "Unlocked after your first recorded offer. A red-night terminal mode is now available in Mission Control.", insights.Offers >= 1},
	}
	var events []eggEvent
	for _, check := range checks {
		if check.unlocked && !data.EasterEggs.UnlockedThemes[check.key] {
			data.EasterEggs.UnlockedThemes[check.key] = true
			events = append(events, eggEvent{Kind: "theme", Title: "VISUAL MODULE UNLOCKED", Detail: check.name + " // " + check.detail})
		}
	}
	return events
}

func availableThemes(state easterEggs) []themeChoice {
	choices := []themeChoice{{Key: "ocean", Name: "OCEAN CRT", Description: "electric-blue enterprise console"}}
	if state.UnlockedThemes["amber"] {
		choices = append(choices, themeChoice{Key: "amber", Name: "AMBER FOUNDRY", Description: "copper, brass, and warm CRT phosphor"})
	}
	if state.UnlockedThemes["phosphor"] {
		choices = append(choices, themeChoice{Key: "phosphor", Name: "PHOSPHOR VECTOR", Description: "green-screen signal terminal"})
	}
	if state.UnlockedThemes["ruby"] {
		choices = append(choices, themeChoice{Key: "ruby", Name: "RUBY SYNTHWAVE", Description: "red-night data console"})
	}
	return choices
}

func (m model) View() string {
	switch m.view {
	case bootScreen:
		return m.viewBoot()
	case eggScreen:
		return m.viewEgg()
	case themeScreen:
		return m.viewThemePicker()
	case formScreen:
		return m.viewForm()
	case goalsScreen:
		return m.viewGoalsForm()
	case statusScreen:
		return m.viewStatusPicker()
	case deleteScreen:
		return m.viewDeletePicker()
	default:
		if m.section == insightsSection {
			return m.viewInsights()
		}
		if m.section == timelineSection {
			return m.viewTimeline()
		}
		if m.section == geographySection {
			return m.viewGeography()
		}
		if m.section == missionSection {
			return m.viewMissionControl()
		}
		return m.viewList()
	}
}

func (m model) viewBoot() string {
	brass := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFE1A5")).Bold(true)
	copper := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9A3E")).Bold(true)
	ember := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4C2B")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#A27B56"))
	steps := []string{
		"VACUUM TUBES                  WARMING",
		"BRASS BUS                     ONLINE",
		"OPPORTUNITY LEDGER            MOUNTED",
		"LOCAL STORAGE                 VERIFIED",
		"NETWORK SIGNALS               READY",
	}
	if m.bootPhase > len(steps) {
		m.bootPhase = len(steps)
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(copper.Render("        ╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲\n"))
	b.WriteString(copper.Render("          ╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲╲\n"))
	b.WriteString(brass.Render("              C A R E E R   H U B\n"))
	b.WriteString(brass.Render("           PERSONAL OPPORTUNITY ENGINE\n"))
	b.WriteString(ember.Render("              //  STEAMWORKS 1984  //\n"))
	b.WriteString(copper.Render("          ╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱\n"))
	b.WriteString(copper.Render("        ╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱\n\n"))
	b.WriteString(dim.Render("   ┌────────────────────────────────────────────────────┐\n"))
	for index, step := range steps {
		if index < m.bootPhase {
			b.WriteString(dim.Render("   │ ") + brass.Render("◆ ") + inputStyle.Render(padRight(step, 47)) + dim.Render(" │\n"))
		} else {
			b.WriteString(dim.Render("   │   ") + dim.Render(padRight(step, 47)) + dim.Render(" │\n"))
		}
	}
	b.WriteString(dim.Render("   └────────────────────────────────────────────────────┘\n\n"))
	if m.bootPhase >= len(steps) {
		b.WriteString(copper.Render("   SYSTEM ARMED  ▸  PRESS ANY KEY TO ENTER\n"))
	} else {
		b.WriteString(dim.Render("   INITIALIZING ELECTRO-MECHANICAL CONSOLE...\n"))
	}
	return b.String()
}

func (m model) viewEgg() string {
	brass := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFE1A5")).Bold(true)
	copper := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9A3E")).Bold(true)
	ember := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4C2B")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#A27B56"))
	icon := "◆"
	if m.egg.Kind == "reply" {
		icon = "◉"
	}
	if m.egg.Kind == "offer" {
		icon = "✦"
	}
	if m.egg.Kind == "theme" {
		icon = "◈"
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(ember.Render("   ╔══════════════════════════════════════════════════════╗\n"))
	b.WriteString(ember.Render("   ║") + brass.Render("              CAREER HUB // SPECIAL SIGNAL              ") + ember.Render("║\n"))
	b.WriteString(ember.Render("   ╠══════════════════════════════════════════════════════╣\n"))
	b.WriteString(ember.Render("   ║                                                      ║\n"))
	b.WriteString(ember.Render("   ║   ") + copper.Render(icon+"  "+padRight(m.egg.Title, 47)) + ember.Render("║\n"))
	b.WriteString(ember.Render("   ║                                                      ║\n"))
	for _, line := range wrapTerminalText(m.egg.Detail, 48) {
		b.WriteString(ember.Render("   ║   ") + brass.Render(padRight(line, 51)) + ember.Render("║\n"))
	}
	b.WriteString(ember.Render("   ║                                                      ║\n"))
	b.WriteString(ember.Render("   ╠══════════════════════════════════════════════════════╣\n"))
	b.WriteString(ember.Render("   ║ ") + dim.Render("     PRESS ANY KEY TO ACKNOWLEDGE SIGNAL                ") + ember.Render(" ║\n"))
	b.WriteString(ember.Render("   ╚══════════════════════════════════════════════════════╝\n"))
	return b.String()
}

func (m model) viewThemePicker() string {
	choices := availableThemes(m.data.EasterEggs)
	var b strings.Builder
	b.WriteString(m.header("VISUAL MODULE BAY", "UNLOCKED TERMINAL THEMES  /  SELECT YOUR CONSOLE PALETTE"))
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")
	for index, choice := range choices {
		prefix := "  "
		name := padRight(choice.Name, 20)
		if index == m.themeCursor {
			prefix = "▸ "
			b.WriteString(selectStyle.Render(prefix + name + "  " + choice.Description))
		} else {
			b.WriteString(systemStyle.Render(prefix) + inputStyle.Render(name) + mutedStyle.Render("  "+choice.Description))
		}
		if choice.Key == activeTheme(m.data.EasterEggs.Theme) {
			b.WriteString(goodStyle.Render("  ACTIVE"))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Themes are unlocked only by real tracker milestones. They do not alter your records."))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("↑↓/jk select  Enter activate  Esc cancel"))
	return b.String()
}

func wrapTerminalText(value string, width int) []string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := ""
	for _, word := range words {
		candidate := strings.TrimSpace(line + " " + word)
		if utf8.RuneCountInString(candidate) > width && line != "" {
			lines = append(lines, line)
			line = word
			continue
		}
		line = candidate
	}
	return append(lines, line)
}

func (m model) viewList() string {
	var b strings.Builder
	b.WriteString(m.header("CAREER HUB", "PERSONAL OPS  /  LOCAL-FIRST CAREER CONSOLE"))
	b.WriteString("\n")
	b.WriteString(m.sectionToggle())
	b.WriteString("\n")
	for i, tab := range m.sectionTabs() {
		label := fmt.Sprintf("%s (%d)", strings.ToUpper(tab), m.count(tab))
		if i == m.tab {
			b.WriteString(selectStyle.Render(" " + label + " "))
		} else {
			b.WriteString(mutedStyle.Render(" " + label + " "))
		}
		b.WriteString(" ")
	}
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")

	visible := m.visibleIndices()
	if len(visible) == 0 {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("No records in this view. Press "))
		b.WriteString(keyStyle.Render("a"))
		b.WriteString(mutedStyle.Render(" to add one."))
	} else if m.section == applicationsSection {
		b.WriteString(m.viewApplicationsTable(visible))
	} else if m.section == networkingSection {
		b.WriteString(m.viewContactsTable(visible))
	} else {
		b.WriteString(m.viewTasksTable(visible))
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(m.renderMessage())
		b.WriteString("\n")
	}
	b.WriteString(footerStyle.Render("1/2/3/4/5/6/7 section  ↑↓/jk navigate  ←→/hl tabs  "))
	b.WriteString(keyStyle.Render("a"))
	b.WriteString(footerStyle.Render(" add  "))
	b.WriteString(keyStyle.Render("e"))
	b.WriteString(footerStyle.Render(" edit  "))
	b.WriteString(keyStyle.Render("c"))
	b.WriteString(footerStyle.Render(" status  "))
	b.WriteString(keyStyle.Render("x"))
	if m.section == tasksSection {
		b.WriteString(footerStyle.Render(" complete  "))
	} else {
		b.WriteString(footerStyle.Render(" archive  "))
	}
	b.WriteString(keyStyle.Render("D"))
	b.WriteString(footerStyle.Render(" delete  "))
	if m.section != tasksSection {
		b.WriteString(keyStyle.Render("o"))
		b.WriteString(footerStyle.Render(" open URL  "))
	}
	b.WriteString(keyStyle.Render("q"))
	b.WriteString(footerStyle.Render(" quit"))
	return b.String()
}

func (m model) sectionToggle() string {
	sections := []struct {
		section section
		label   string
	}{
		{applicationsSection, " 1 APPLICATIONS "},
		{networkingSection, " 2 NETWORKING "},
		{insightsSection, " 3 INSIGHTS "},
		{timelineSection, " 4 TIMELINE "},
		{geographySection, " 5 GEOGRAPHY "},
		{missionSection, " 6 MISSION CONTROL "},
		{tasksSection, " 7 TASK PAD "},
	}
	if m.width < 108 {
		sections = []struct {
			section section
			label   string
		}{
			{applicationsSection, " 1 APPS "},
			{networkingSection, " 2 NET "},
			{insightsSection, " 3 INSIGHTS "},
			{timelineSection, " 4 LOG "},
			{geographySection, " 5 MAP "},
			{missionSection, " 6 MISSION "},
			{tasksSection, " 7 TASKS "},
		}
	}
	var b strings.Builder
	for i, item := range sections {
		if i > 0 {
			b.WriteString(" ")
		}
		if m.section == item.section {
			b.WriteString(selectStyle.Render("▐" + item.label))
		} else {
			b.WriteString(systemStyle.Render(item.label))
		}
	}
	return b.String()
}

func (m model) viewApplicationsTable(visible []int) string {
	var b strings.Builder
	b.WriteString(tableHeaderStyle.Render(fmt.Sprintf("   %-4s %-11s %-21s %-33s %-13s %-17s", "#", "DATE", "COMPANY", "ROLE", "STATUS", "LOCATION")))
	b.WriteString("\n")
	rows, cursor := max(4, m.height-16), min(m.cursor, len(visible)-1)
	start := max(0, cursor-rows/2)
	end := min(len(visible), start+rows)
	if end-start < rows {
		start = max(0, end-rows)
	}
	for pos := start; pos < end; pos++ {
		j := m.data.Applications[visible[pos]]
		line := fmt.Sprintf("%-4d %-11s %-21s %-33s %-13s %-17s", j.ID, truncate(j.Date, 10), truncate(j.Company, 21), truncate(j.Role, 33), truncate(j.Status, 13), truncate(j.Location, 17))
		// A full-row background looks nice in some terminals but is rendered at
		// the wrong horizontal position in others. Keep the fixed-width data
		// line plain and use an IBM-style phosphor cursor in its own column.
		b.WriteString(rowMarker(pos == cursor))
		b.WriteString(line)
		b.WriteString("\n")
	}
	if j, ok := m.selectedApplication(); ok {
		b.WriteString("\n" + brandStyle.Render("▸ SELECTED") + "  " + j.Company + " — " + j.Role + "  " + statusStyle(j.Status).Render(j.Status) + "\n")
		source := j.Source
		if source == "" {
			source = "Unspecified"
		}
		b.WriteString(systemStyle.Render("  SOURCE ") + inputStyle.Render(truncate(source, max(40, m.width-12))) + "\n")
		b.WriteString(systemStyle.Render("  NOTES  ") + mutedStyle.Render(truncate(j.Notes, max(40, m.width-10))) + "\n")
	}
	return b.String()
}

func (m model) viewContactsTable(visible []int) string {
	var b strings.Builder
	b.WriteString(tableHeaderStyle.Render(fmt.Sprintf("   %-4s %-21s %-20s %-25s %-15s %-14s", "#", "NAME", "COMPANY", "TITLE", "STATUS", "FOLLOW-UP")))
	b.WriteString("\n")
	rows, cursor := max(4, m.height-16), min(m.cursor, len(visible)-1)
	start := max(0, cursor-rows/2)
	end := min(len(visible), start+rows)
	if end-start < rows {
		start = max(0, end-rows)
	}
	for pos := start; pos < end; pos++ {
		c := m.data.Contacts[visible[pos]]
		line := fmt.Sprintf("%-4d %-21s %-20s %-25s %-15s %-14s", c.ID, truncate(c.Name, 21), truncate(c.Company, 20), truncate(c.Title, 25), truncate(c.Status, 15), truncate(c.NextFollowup, 14))
		b.WriteString(rowMarker(pos == cursor))
		b.WriteString(line)
		b.WriteString("\n")
	}
	if c, ok := m.selectedContact(); ok {
		b.WriteString("\n" + brandStyle.Render("▸ SELECTED") + "  " + c.Name + " — " + c.Company + "  " + statusStyle(c.Status).Render(c.Status) + "\n")
		b.WriteString(systemStyle.Render("  CONTEXT  ") + mutedStyle.Render(truncate(c.Relationship, max(40, m.width-12))) + "\n")
		b.WriteString(systemStyle.Render("  NOTES    ") + mutedStyle.Render(truncate(c.Notes, max(40, m.width-12))) + "\n")
	}
	return b.String()
}

func (m model) viewTasksTable(visible []int) string {
	var b strings.Builder
	b.WriteString(tableHeaderStyle.Render(fmt.Sprintf("   %-4s %-7s %-72s %-20s", "#", "STATE", "TASK NOTE", "UPDATED")))
	b.WriteString("\n")
	rows, cursor := max(4, m.height-16), min(m.cursor, len(visible)-1)
	start := max(0, cursor-rows/2)
	end := min(len(visible), start+rows)
	if end-start < rows {
		start = max(0, end-rows)
	}
	for pos := start; pos < end; pos++ {
		t := m.data.Tasks[visible[pos]]
		state := "OPEN"
		if t.Done {
			state = "DONE"
		}
		line := fmt.Sprintf("%-4d %-7s %-72s %-20s", t.ID, state, truncate(t.Text, 72), formatActivityTime(t.UpdatedAt))
		b.WriteString(rowMarker(pos == cursor))
		if t.Done {
			b.WriteString(mutedStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if t, ok := m.selectedTask(); ok {
		state := "OPEN"
		if t.Done {
			state = "DONE"
		}
		b.WriteString("\n" + brandStyle.Render("▸ NOTE") + "  " + inputStyle.Render(t.Text) + "  " + statusStyle(state).Render(state) + "\n")
		b.WriteString(systemStyle.Render("  CREATED ") + mutedStyle.Render(formatActivityTime(t.CreatedAt)) + "\n")
	}
	return b.String()
}

func (m model) viewGeography() string {
	stats := buildGeographyStats(m.data)
	states := sortedStateCounts(stats.States)

	var b strings.Builder
	b.WriteString(m.header("US GEOGRAPHY", "SUBMITTED APPLICATIONS BY STATE"))
	b.WriteString("\n")
	b.WriteString(m.sectionToggle())
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")

	b.WriteString(tableHeaderStyle.Render("U.S. STATE BOUNDARIES  /  ● = ONE OR MORE SUBMITTED APPLICATIONS"))
	b.WriteString("\n")
	if m.width < 64 {
		b.WriteString(warnStyle.Render("Enlarge this terminal to at least 64 columns to show the U.S. boundary map."))
		b.WriteString("\n")
	} else {
		mapView, err := renderUSOutlineMap(stats.States, m.width)
		if err != nil {
			b.WriteString(badStyle.Render("Could not load the bundled U.S. state boundaries: " + err.Error()))
			b.WriteString("\n")
		} else {
			b.WriteString(mapView)
		}
	}
	b.WriteString("\n")
	b.WriteString(metric("SUBMITTED", stats.Submitted))
	b.WriteString("   ")
	b.WriteString(metric("STATES", len(stats.States)))
	b.WriteString("   ")
	b.WriteString(metric("REMOTE", stats.Remote))
	b.WriteString("   ")
	b.WriteString(metric("UNMAPPED", stats.Unknown))
	b.WriteString("\n\n")

	b.WriteString(tableHeaderStyle.Render("STATE COUNTS"))
	b.WriteString("\n")
	if len(states) == 0 {
		b.WriteString(mutedStyle.Render("No submitted applications have a recognizable U.S. state yet."))
		b.WriteString("\n")
	} else {
		for _, state := range states[:min(10, len(states))] {
			b.WriteString(brandStyle.Render("● "))
			b.WriteString(keyStyle.Render(state.Code + "  "))
			b.WriteString(inputStyle.Render(padRight(state.Name, 22)))
			b.WriteString(goodStyle.Render(fmt.Sprintf("%d", state.Count)))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("The map is rendered from bundled U.S. state boundary data. State dots use each state boundary's bounding-box center. Location accepts Seattle, WA or Austin, Texas; Remote and unrecognized locations remain separate."))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("1/2/3/4/5/6/7 section  r reload local data  q quit"))
	return b.String()
}

func renderUSOutlineMap(counts map[string]int, terminalWidth int) (string, error) {
	outlines, err := usStateOutlines()
	if err != nil {
		return "", err
	}

	// Braille cells give us a 2×4 drawing grid per terminal character, keeping
	// actual state borders legible without needing a graphical window.
	contiguousWidth := min(92, max(62, terminalWidth-4))
	contiguous := renderOutlineRegion(outlines, counts, func(code string) bool {
		return code != "AK" && code != "HI"
	}, -125.2, -66.2, 24.0, 50.7, contiguousWidth, 17)
	alaska := renderOutlineRegion(outlines, counts, func(code string) bool { return code == "AK" }, -180.0, -129.0, 50.0, 72.5, 22, 4)
	hawaii := renderOutlineRegion(outlines, counts, func(code string) bool { return code == "HI" }, -161.0, -154.0, 18.0, 23.0, 14, 4)

	var b strings.Builder
	b.WriteString(systemStyle.Render("  CONTIGUOUS U.S."))
	b.WriteString("\n")
	for _, line := range contiguous {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(systemStyle.Render("  ALASKA"))
	b.WriteString(strings.Repeat(" ", 18))
	b.WriteString(systemStyle.Render("HAWAII"))
	b.WriteString("\n")
	for row := 0; row < max(len(alaska), len(hawaii)); row++ {
		if row < len(alaska) {
			b.WriteString(alaska[row])
		}
		b.WriteString(strings.Repeat(" ", max(1, 40-lipgloss.Width(alaskaLine(alaska, row)))))
		if row < len(hawaii) {
			b.WriteString(hawaii[row])
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func alaskaLine(lines []string, row int) string {
	if row < len(lines) {
		return lines[row]
	}
	return ""
}

func renderOutlineRegion(outlines map[string]stateOutline, counts map[string]int, include func(string) bool, minLon, maxLon, minLat, maxLat float64, width, height int) []string {
	canvas := newBrailleCanvas(width, height)
	for code, outline := range outlines {
		if !include(code) {
			continue
		}
		for _, line := range outline.Lines {
			for index := 1; index < len(line); index++ {
				x0, y0 := projectGeoPoint(line[index-1], minLon, maxLon, minLat, maxLat, canvas.Width*2, canvas.Height*4)
				x1, y1 := projectGeoPoint(line[index], minLon, maxLon, minLat, maxLat, canvas.Width*2, canvas.Height*4)
				canvas.drawLine(x0, y0, x1, y1)
			}
		}
		if counts[code] > 0 {
			x, y := projectGeoPoint(outline.Center, minLon, maxLon, minLat, maxLat, canvas.Width*2, canvas.Height*4)
			canvas.addMarker(x/2, y/4)
		}
	}
	return canvas.render()
}

func projectGeoPoint(point geoPoint, minLon, maxLon, minLat, maxLat float64, dotWidth, dotHeight int) (int, int) {
	x := int(math.Round((point.Lon - minLon) / (maxLon - minLon) * float64(dotWidth-1)))
	y := int(math.Round((maxLat - point.Lat) / (maxLat - minLat) * float64(dotHeight-1)))
	return clampInt(x, 0, dotWidth-1), clampInt(y, 0, dotHeight-1)
}

func usStateOutlines() (map[string]stateOutline, error) {
	outlineOnce.Do(func() {
		outlineData, outlineErr = parseUSStateOutlines(usStatesTopoJSON)
	})
	return outlineData, outlineErr
}

func parseUSStateOutlines(raw []byte) (map[string]stateOutline, error) {
	var topology topoTopology
	if err := json.Unmarshal(raw, &topology); err != nil {
		return nil, fmt.Errorf("parse TopoJSON: %w", err)
	}
	states, ok := topology.Objects["states"]
	if !ok {
		return nil, errors.New("states object is missing")
	}
	if len(topology.Arcs) == 0 {
		return nil, errors.New("state border arcs are missing")
	}

	decodedArcs := make([][]geoPoint, len(topology.Arcs))
	for index, arc := range topology.Arcs {
		decodedArcs[index] = decodeTopoArc(arc, topology.Transform)
	}

	outlines := make(map[string]stateOutline)
	for _, geometry := range states.Geometries {
		code := stateCodeForName(geometry.Properties.Name)
		if code == "" {
			continue // Territories are intentionally not included in the U.S. state view.
		}
		arcIndexes, err := topoArcIndexes(geometry.Arcs)
		if err != nil {
			return nil, fmt.Errorf("read %s geometry: %w", geometry.Properties.Name, err)
		}

		outline := stateOutline{Code: code}
		minX, maxX := math.Inf(1), math.Inf(-1)
		minY, maxY := math.Inf(1), math.Inf(-1)
		for arcIndex := range arcIndexes {
			if arcIndex < 0 || arcIndex >= len(decodedArcs) {
				return nil, fmt.Errorf("%s refers to missing border arc %d", geometry.Properties.Name, arcIndex)
			}
			line := decodedArcs[arcIndex]
			if len(line) < 2 {
				continue
			}
			outline.Lines = append(outline.Lines, line)
			for _, point := range line {
				minX = math.Min(minX, point.Lon)
				maxX = math.Max(maxX, point.Lon)
				minY = math.Min(minY, point.Lat)
				maxY = math.Max(maxY, point.Lat)
			}
		}
		if len(outline.Lines) == 0 {
			return nil, fmt.Errorf("%s has no drawable border", geometry.Properties.Name)
		}
		outline.Center = geoPoint{Lon: (minX + maxX) / 2, Lat: (minY + maxY) / 2}
		outlines[code] = outline
	}
	if len(outlines) < 51 {
		return nil, fmt.Errorf("only found %d state outlines", len(outlines))
	}
	return outlines, nil
}

func decodeTopoArc(arc [][]int, transform topoTransform) []geoPoint {
	points := make([]geoPoint, 0, len(arc))
	x, y := 0, 0
	for _, delta := range arc {
		if len(delta) < 2 {
			continue
		}
		x += delta[0]
		y += delta[1]
		points = append(points, geoPoint{
			Lon: float64(x)*transform.Scale[0] + transform.Translate[0],
			Lat: float64(y)*transform.Scale[1] + transform.Translate[1],
		})
	}
	return points
}

func topoArcIndexes(raw json.RawMessage) (map[int]bool, error) {
	var nested any
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil, err
	}
	indexes := make(map[int]bool)
	collectTopoArcIndexes(nested, indexes)
	return indexes, nil
}

func collectTopoArcIndexes(value any, indexes map[int]bool) {
	switch item := value.(type) {
	case float64:
		index := int(item)
		if index < 0 {
			index = ^index // TopoJSON uses one's complement for reversed arcs.
		}
		indexes[index] = true
	case []any:
		for _, child := range item {
			collectTopoArcIndexes(child, indexes)
		}
	}
}

func stateCodeForName(name string) string {
	for code, stateName := range stateNames {
		if strings.EqualFold(name, stateName) {
			return code
		}
	}
	return ""
}

func newBrailleCanvas(width, height int) *brailleCanvas {
	dots := make([][]bool, height*4)
	for row := range dots {
		dots[row] = make([]bool, width*2)
	}
	return &brailleCanvas{Width: width, Height: height, Dots: dots, Markers: make(map[[2]int]bool)}
}

func (canvas *brailleCanvas) setDot(x, y int) {
	if y >= 0 && y < len(canvas.Dots) && x >= 0 && x < len(canvas.Dots[y]) {
		canvas.Dots[y][x] = true
	}
}

func (canvas *brailleCanvas) drawLine(x0, y0, x1, y1 int) {
	dx := absInt(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -absInt(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		canvas.setDot(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		errorTwice := 2 * err
		if errorTwice >= dy {
			err += dy
			x0 += sx
		}
		if errorTwice <= dx {
			err += dx
			y0 += sy
		}
	}
}

func (canvas *brailleCanvas) addMarker(x, y int) {
	canvas.Markers[[2]int{clampInt(x, 0, canvas.Width-1), clampInt(y, 0, canvas.Height-1)}] = true
}

func (canvas *brailleCanvas) render() []string {
	lines := make([]string, canvas.Height)
	for row := 0; row < canvas.Height; row++ {
		var line strings.Builder
		for column := 0; column < canvas.Width; column++ {
			if canvas.Markers[[2]int{column, row}] {
				line.WriteString(brandStyle.Render("●"))
				continue
			}
			bits := canvas.brailleBits(column, row)
			if bits == 0 {
				line.WriteString(" ")
			} else {
				line.WriteString(frameStyle.Render(string(rune(0x2800 + bits))))
			}
		}
		lines[row] = line.String()
	}
	return lines
}

func (canvas *brailleCanvas) brailleBits(column, row int) int {
	x, y := column*2, row*4
	bits := 0
	if canvas.Dots[y][x] {
		bits |= 1
	}
	if canvas.Dots[y+1][x] {
		bits |= 2
	}
	if canvas.Dots[y+2][x] {
		bits |= 4
	}
	if canvas.Dots[y][x+1] {
		bits |= 8
	}
	if canvas.Dots[y+1][x+1] {
		bits |= 16
	}
	if canvas.Dots[y+2][x+1] {
		bits |= 32
	}
	if canvas.Dots[y+3][x] {
		bits |= 64
	}
	if canvas.Dots[y+3][x+1] {
		bits |= 128
	}
	return bits
}

func clampInt(value, lower, upper int) int {
	return min(upper, max(lower, value))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func buildGeographyStats(data dataFile) geographyStats {
	stats := geographyStats{States: make(map[string]int)}
	for _, job := range data.Applications {
		if strings.EqualFold(job.Status, "Archived") || !isSubmitted(job.Status) {
			continue
		}
		stats.Submitted++
		if state, remote := stateFromLocation(job.Location); state != "" {
			stats.States[state]++
		} else if remote {
			stats.Remote++
		} else {
			stats.Unknown++
		}
	}
	return stats
}

func stateFromLocation(location string) (string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(location))
	if upper == "" {
		return "", false
	}
	if strings.Contains(upper, "REMOTE") {
		return "", true
	}
	for _, token := range strings.FieldsFunc(upper, func(r rune) bool { return r < 'A' || r > 'Z' }) {
		if _, ok := stateNames[token]; ok {
			return token, false
		}
	}

	lower := strings.ToLower(location)
	if strings.Contains(lower, "district of columbia") || strings.Contains(lower, "washington, d.c") || strings.Contains(lower, "washington dc") {
		return "DC", false
	}
	if strings.Contains(lower, "west virginia") {
		return "WV", false
	}
	for code, name := range stateNames {
		if code == "DC" || code == "WV" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(name)) {
			return code, false
		}
	}
	return "", false
}

func sortedStateCounts(counts map[string]int) []stateCount {
	states := make([]stateCount, 0, len(counts))
	for code, count := range counts {
		states = append(states, stateCount{Code: code, Name: stateNames[code], Count: count})
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].Count == states[j].Count {
			return states[i].Code < states[j].Code
		}
		return states[i].Count > states[j].Count
	})
	return states
}

func (m model) viewTimeline() string {
	events := m.filteredActivities()
	var b strings.Builder
	b.WriteString(m.header("ACTIVITY TIMELINE", "LOCAL AUDIT LOG  /  NO FABRICATED HISTORY"))
	b.WriteString("\n")
	b.WriteString(m.sectionToggle())
	b.WriteString("\n")
	for i, tab := range m.sectionTabs() {
		label := fmt.Sprintf("%s (%d)", strings.ToUpper(tab), m.count(tab))
		if i == m.tab {
			b.WriteString(selectStyle.Render(" " + label + " "))
		} else {
			b.WriteString(mutedStyle.Render(" " + label + " "))
		}
		b.WriteString(" ")
	}
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")

	if len(events) == 0 {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("No activity has been recorded yet."))
		b.WriteString("\n")
		b.WriteString(systemStyle.Render("Timeline recording starts with your next application, networking, or task-pad action."))
	} else {
		b.WriteString(tableHeaderStyle.Render("  WHEN                 SUBJECT                         ACTION              DETAIL"))
		b.WriteString("\n")
		rows := min(len(events), max(5, m.height-12))
		detailWidth := max(18, m.width-76)
		for _, event := range events[:rows] {
			b.WriteString(keyStyle.Render("▸ "))
			b.WriteString(systemStyle.Render(padRight(formatActivityTime(event.OccurredAt), 20)))
			b.WriteString(" ")
			b.WriteString(inputStyle.Render(padRight(event.Subject, 31)))
			b.WriteString(" ")
			b.WriteString(activityActionStyle(event.Action).Render(padRight(event.Action, 18)))
			b.WriteString(" ")
			b.WriteString(mutedStyle.Render(truncate(event.Detail, detailWidth)))
			b.WriteString("\n")
		}
		if len(events) > rows {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("… %d older event(s) are stored locally.", len(events)-rows)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Events are saved in career-hub.json. Use ←/→ or h/l to filter Applications, Networking, or Tasks."))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("1/2/3/4/5/6/7 section  ←→/hl filter  r reload local data  q quit"))
	return b.String()
}

func (m model) filteredActivities() []activity {
	filter := m.sectionTabs()[m.tab]
	events := make([]activity, 0, len(m.data.Activities))
	for _, event := range m.data.Activities {
		if filter == "All" || strings.EqualFold(event.EntityType, activityEntityType(filter)) {
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		return activityTime(events[i]).After(activityTime(events[j]))
	})
	return events
}

func (m model) activityCount(tab string) int {
	if tab == "All" {
		return len(m.data.Activities)
	}
	count := 0
	for _, event := range m.data.Activities {
		if strings.EqualFold(event.EntityType, activityEntityType(tab)) {
			count++
		}
	}
	return count
}

func activityEntityType(tab string) string {
	switch tab {
	case "Applications":
		return "Application"
	case "Tasks":
		return "Task"
	default:
		return tab
	}
}

func activityActionStyle(action string) lipgloss.Style {
	switch action {
	case "Created":
		return goodStyle
	case "Deleted":
		return badStyle
	case "Status changed":
		return warnStyle
	default:
		return keyStyle
	}
}

func activityTime(event activity) time.Time {
	value, err := time.Parse(time.RFC3339, event.OccurredAt)
	if err != nil {
		return time.Time{}
	}
	return value
}

func formatActivityTime(value string) string {
	if timestamp := activityTime(activity{OccurredAt: value}); !timestamp.IsZero() {
		return timestamp.In(time.Local).Format("Jan 02 15:04")
	}
	return truncate(value, 20)
}

func (m model) viewInsights() string {
	if m.tab == 1 {
		return m.viewSourceAttribution()
	}
	stats := buildInsightStats(m.data, time.Now())
	active := stats.Applied + stats.Interviews

	var b strings.Builder
	b.WriteString(m.header("CAREER INSIGHTS", "SUBMISSIONS, PIPELINE, AND NETWORKING HEALTH"))
	b.WriteString("\n")
	b.WriteString(m.sectionToggle())
	b.WriteString("\n")
	b.WriteString(m.renderViewTabs(false))
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")

	b.WriteString(tableHeaderStyle.Render("APPLICATION STATISTICS"))
	b.WriteString("\n")
	b.WriteString(metric("TRACKED", stats.Tracked))
	b.WriteString("   ")
	b.WriteString(metric("SUBMITTED", stats.Submitted))
	b.WriteString("   ")
	b.WriteString(metric("ACTIVE", active))
	b.WriteString("   ")
	b.WriteString(metric("THIS WEEK", latestBucketCount(stats.Weekly)))
	b.WriteString("   ")
	b.WriteString(metric("THIS MONTH", latestBucketCount(stats.Monthly)))
	b.WriteString("\n")
	b.WriteString(systemStyle.Render("  INTERVIEW RATE "))
	b.WriteString(goodStyle.Render(percent(stats.Interviews+stats.Offers, stats.Submitted)))
	b.WriteString(systemStyle.Render("  ·  OFFER RATE "))
	b.WriteString(goodStyle.Render(percent(stats.Offers, stats.Submitted)))
	b.WriteString(systemStyle.Render("  ·  PROSPECTS "))
	b.WriteString(inputStyle.Render(fmt.Sprintf("%d", stats.Prospects)))
	b.WriteString("\n\n")

	b.WriteString(renderTrend("WEEKLY SUBMISSIONS  /  LAST 6 WEEKS", stats.Weekly, m.width))
	b.WriteString("\n")
	b.WriteString(renderTrend("MONTHLY SUBMISSIONS  /  LAST 6 MONTHS", stats.Monthly, m.width))
	b.WriteString("\n")

	b.WriteString(tableHeaderStyle.Render("CURRENT PIPELINE"))
	b.WriteString("\n")
	b.WriteString(metric("PROSPECT", stats.Prospects))
	b.WriteString("   ")
	b.WriteString(metric("APPLIED", stats.Applied))
	b.WriteString("   ")
	b.WriteString(metric("INTERVIEW", stats.Interviews))
	b.WriteString("   ")
	b.WriteString(metric("OFFER", stats.Offers))
	b.WriteString("   ")
	b.WriteString(metric("REJECTED", stats.Rejected))
	b.WriteString("\n\n")

	b.WriteString(tableHeaderStyle.Render("NETWORKING HEALTH"))
	b.WriteString("\n")
	b.WriteString(metric("CONTACTS", stats.Contacts))
	b.WriteString("   ")
	b.WriteString(metric("OUTREACH", stats.Outreach))
	b.WriteString("   ")
	b.WriteString(metric("REPLIES", stats.Replies))
	b.WriteString("   ")
	b.WriteString(metric("MEETINGS", stats.Meetings))
	b.WriteString("   ")
	b.WriteString(metric("FOLLOW-UPS DUE", stats.FollowupDue))
	b.WriteString("\n")

	if stats.Undated > 0 {
		b.WriteString(warnStyle.Render(fmt.Sprintf("NOTE  %d submitted record(s) have no readable application date and are excluded from the trend charts.", stats.Undated)))
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render("Charts count records with status Applied, Interview, Offer, or Rejected. New status changes record the application date automatically."))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("1/2/3/4/5/6/7 section  ←→/hl view  r reload local data  q quit"))
	return b.String()
}

func (m model) viewSourceAttribution() string {
	sources := buildSourceStats(m.data)
	tracked, submitted, advanced, offers := 0, 0, 0, 0
	for _, source := range sources {
		tracked += source.Tracked
		submitted += source.Submitted
		advanced += source.Advanced
		offers += source.Offers
	}

	var b strings.Builder
	b.WriteString(m.header("CAREER INSIGHTS", "CHANNEL ATTRIBUTION  /  WHICH SOURCES CREATE MOMENTUM"))
	b.WriteString("\n")
	b.WriteString(m.sectionToggle())
	b.WriteString("\n")
	b.WriteString(m.renderViewTabs(false))
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")
	b.WriteString(metric("CHANNELS", len(sources)))
	b.WriteString("   ")
	b.WriteString(metric("TRACKED", tracked))
	b.WriteString("   ")
	b.WriteString(metric("SUBMITTED", submitted))
	b.WriteString("   ")
	b.WriteString(metric("ADVANCED", advanced))
	b.WriteString("   ")
	b.WriteString(metric("OFFERS", offers))
	b.WriteString("\n\n")

	if len(sources) == 0 {
		b.WriteString(mutedStyle.Render("No active applications yet. Add a job, then enter its Source when you edit it."))
		b.WriteString("\n")
	} else {
		b.WriteString(renderSourceAttribution(sources, m.width))
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Edit an application with e to set Source. Use consistent labels such as LinkedIn, Company site, Referral, Recruiter, or Indeed. Older records remain Unspecified until you update them."))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("1/2/3/4/5/6/7 section  ←→/hl view  r reload local data  q quit"))
	return b.String()
}

func (m model) renderViewTabs(includeCounts bool) string {
	var b strings.Builder
	for index, tab := range m.sectionTabs() {
		label := strings.ToUpper(tab)
		if includeCounts {
			label = fmt.Sprintf("%s (%d)", label, m.count(tab))
		}
		if index == m.tab {
			b.WriteString(selectStyle.Render(" " + label + " "))
		} else {
			b.WriteString(mutedStyle.Render(" " + label + " "))
		}
		b.WriteString(" ")
	}
	return b.String()
}

func (m model) viewMissionControl() string {
	stats := buildMissionStats(m.data, time.Now())
	journeyLimit := 4
	if m.height < 34 {
		journeyLimit = 2
	}
	if m.height < 27 {
		journeyLimit = 1
	}

	var b strings.Builder
	b.WriteString(m.header("MISSION CONTROL", "QUESTS, MOMENTUM, AND REAL JOB-SEARCH ACTIONS"))
	b.WriteString("\n")
	b.WriteString(m.sectionToggle())
	b.WriteString("\n")
	b.WriteString(frameStyle.Render(strings.Repeat("═", max(50, m.width-2))))
	b.WriteString("\n")

	b.WriteString(tableHeaderStyle.Render("DAILY TERMINAL BRIEFING"))
	b.WriteString("\n")
	b.WriteString(keyStyle.Render("  TODAY // "))
	b.WriteString(inputStyle.Render(truncate(missionBriefing(stats), max(28, m.width-13))))
	b.WriteString("\n")
	b.WriteString(metric("XP", stats.XP))
	b.WriteString("   ")
	b.WriteString(metric("LEVEL", stats.Level))
	b.WriteString("   ")
	b.WriteString(metric("TODAY'S ACTIONS", stats.TodayActions))
	b.WriteString("   ")
	b.WriteString(metric("STREAK", stats.CurrentStreak))
	b.WriteString("   ")
	b.WriteString(metric("BEST", stats.BestStreak))
	b.WriteString("\n")

	b.WriteString(tableHeaderStyle.Render("WEEKLY QUEST BOARD"))
	b.WriteString("\n")
	for _, quest := range stats.Quests {
		b.WriteString(renderQuest(quest, m.width))
		b.WriteString("\n")
	}

	b.WriteString(tableHeaderStyle.Render("ACTIVITY SIGNAL  /  LAST 4 WEEKS"))
	b.WriteString("\n")
	b.WriteString(renderActivityHeatmap(stats.Heatmap, time.Now()))

	b.WriteString(tableHeaderStyle.Render("ACHIEVEMENT BAY"))
	b.WriteString("\n")
	b.WriteString(renderAchievementGrid(stats.Achievements, m.width))

	b.WriteString(tableHeaderStyle.Render("APPLICATION JOURNEYS  /  CURRENT TRACKER PHASE"))
	b.WriteString("\n")
	if len(stats.Journeys) == 0 {
		b.WriteString(mutedStyle.Render("  Add an application to start its journey."))
		b.WriteString("\n")
	} else {
		for _, job := range stats.Journeys[:min(journeyLimit, len(stats.Journeys))] {
			b.WriteString(renderJourney(job, m.width))
			b.WriteString("\n")
		}
	}

	b.WriteString(mutedStyle.Render("Derived only from your local records and timeline events — no synthetic activity."))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("1/2/3/4/5/6/7 section  "))
	b.WriteString(keyStyle.Render("g"))
	b.WriteString(footerStyle.Render(" set goals  "))
	b.WriteString(keyStyle.Render("y"))
	b.WriteString(footerStyle.Render(" themes  r reload local data  q quit"))
	return b.String()
}

func missionBriefing(stats missionStats) string {
	switch {
	case stats.FollowupDue > 0:
		return fmt.Sprintf("%d follow-up(s) are due — one thoughtful message is your highest-leverage next move.", stats.FollowupDue)
	case stats.TodayActions == 0:
		return "Quiet console. Log one real action today to start a fresh momentum streak."
	case stats.CurrentStreak >= 3:
		return fmt.Sprintf("Signal is steady: %d consecutive active days. Protect the cadence, not perfection.", stats.CurrentStreak)
	case stats.WeekApps >= 5:
		return "Application quest complete. Use the remaining week to strengthen follow-ups and preparation."
	default:
		return "Momentum detected. Choose the smallest useful next action and record it when it is real."
	}
}

func buildMissionStats(data dataFile, now time.Time) missionStats {
	now = dateOnly(now)
	insights := buildInsightStats(data, now)
	weekStart := startOfWeek(now)
	activityCounts := make(map[string]int)
	stats := missionStats{FollowupDue: insights.FollowupDue}

	for _, event := range data.Activities {
		eventTime := activityTime(event)
		if eventTime.IsZero() {
			continue
		}
		day := dateOnly(eventTime)
		activityCounts[dateKey(day)]++
		if !day.Before(weekStart) && !day.After(now) {
			stats.WeekActions++
			if strings.EqualFold(event.EntityType, "Networking") {
				stats.WeekNetworking++
			}
		}
	}
	stats.TodayActions = activityCounts[dateKey(now)]
	for _, job := range data.Applications {
		if submitted, ok := submissionDate(job); ok && !submitted.Before(weekStart) && !submitted.After(now) {
			stats.WeekApps++
		}
		if !strings.EqualFold(job.Status, "Archived") {
			stats.Journeys = append(stats.Journeys, job)
		}
	}
	sort.SliceStable(stats.Journeys, func(i, j int) bool {
		left, right := journeyPriority(stats.Journeys[i].Status), journeyPriority(stats.Journeys[j].Status)
		if left == right {
			return stats.Journeys[i].Date > stats.Journeys[j].Date
		}
		return left > right
	})

	stats.CurrentStreak = currentActionStreak(activityCounts, now)
	stats.BestStreak = bestActionStreak(activityCounts)
	start := weekStart.AddDate(0, 0, -21)
	for day := start; day.Before(start.AddDate(0, 0, 28)); day = day.AddDate(0, 0, 1) {
		stats.Heatmap = append(stats.Heatmap, heatmapDay{Date: day, Count: activityCounts[dateKey(day)]})
	}

	geography := buildGeographyStats(data)
	stats.XP = insights.Submitted*20 + insights.Outreach*12 + (insights.Interviews+insights.Offers)*55 + insights.Offers*95 + len(data.Activities)*3
	stats.Level = 1 + stats.XP/150
	goals := resolvedWeeklyGoals(data.Goals)
	stats.Quests = []quest{
		{Name: "APPLICATION RUN", Detail: "submitted applications this week", Progress: stats.WeekApps, Target: goals.Applications},
		{Name: "NETWORKING PULSE", Detail: "recorded networking actions this week", Progress: stats.WeekNetworking, Target: goals.Networking},
		{Name: "CAREER OPS RHYTHM", Detail: "recorded tracker actions this week", Progress: stats.WeekActions, Target: goals.Actions},
	}
	stats.Achievements = []achievement{
		{Name: "FIRST SIGNAL", Detail: "submit your first application", Progress: insights.Submitted, Target: 1},
		{Name: "PIPELINE ONLINE", Detail: "submit five applications", Progress: insights.Submitted, Target: 5},
		{Name: "NETWORK ONLINE", Detail: "move one contact into outreach", Progress: insights.Outreach, Target: 1},
		{Name: "REPLY RECEIVED", Detail: "record a reply", Progress: insights.Replies, Target: 1},
		{Name: "INTERVIEW UNLOCKED", Detail: "reach interview stage", Progress: insights.Interviews + insights.Offers, Target: 1},
		{Name: "MULTI-STATE SEARCH", Detail: "apply across three states", Progress: len(geography.States), Target: 3},
		{Name: "SEVEN-DAY RHYTHM", Detail: "build a seven-day action streak", Progress: stats.BestStreak, Target: 7},
	}
	return stats
}

func dateKey(date time.Time) string {
	return dateOnly(date).Format("2006-01-02")
}

func currentActionStreak(counts map[string]int, now time.Time) int {
	streak := 0
	for day := dateOnly(now); counts[dateKey(day)] > 0; day = day.AddDate(0, 0, -1) {
		streak++
	}
	return streak
}

func bestActionStreak(counts map[string]int) int {
	days := make([]time.Time, 0, len(counts))
	for key, count := range counts {
		if count == 0 {
			continue
		}
		if day, ok := parseTrackerDate(key); ok {
			days = append(days, day)
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	best, current := 0, 0
	for index, day := range days {
		if index == 0 || dateOnly(day).Equal(dateOnly(days[index-1]).AddDate(0, 0, 1)) {
			current++
		} else {
			current = 1
		}
		if current > best {
			best = current
		}
	}
	return best
}

func journeyPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "offer":
		return 5
	case "interview":
		return 4
	case "applied":
		return 3
	case "prospect":
		return 2
	case "rejected":
		return 1
	default:
		return 0
	}
}

func renderQuest(quest quest, width int) string {
	barWidth := min(18, max(8, width-64))
	progress := min(quest.Progress, quest.Target)
	filled := 0
	if quest.Target > 0 {
		filled = (progress*barWidth + quest.Target - 1) / quest.Target
	}
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
	label := fmt.Sprintf("  %-19s %2d/%-2d ", quest.Name, progress, quest.Target)
	if quest.Progress >= quest.Target {
		return goodStyle.Render("  ◆ ") + goodStyle.Render(label+bar) + mutedStyle.Render("  COMPLETE")
	}
	detailWidth := max(10, width-(4+len(label)+len(bar)+3))
	return keyStyle.Render("  ▸ ") + inputStyle.Render(label) + brandStyle.Render(bar) + mutedStyle.Render("  "+truncate(quest.Detail, detailWidth))
}

func renderActivityHeatmap(days []heatmapDay, now time.Time) string {
	var b strings.Builder
	b.WriteString(systemStyle.Render("       M  T  W  T  F  S  S"))
	b.WriteString("\n")
	for week := 0; week < 4; week++ {
		label := fmt.Sprintf("W-%d", 3-week)
		if week == 3 {
			label = "NOW"
		}
		b.WriteString(systemStyle.Render("  " + padRight(label, 4)))
		for day := 0; day < 7; day++ {
			entry := days[week*7+day]
			b.WriteString(" ")
			b.WriteString(renderHeatmapCell(entry, now))
			b.WriteString(" ")
		}
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render("  ▣ today   · no recorded action   ▪ one action   █ two or more"))
	b.WriteString("\n")
	return b.String()
}

func renderHeatmapCell(day heatmapDay, now time.Time) string {
	if dateOnly(day.Date).Equal(dateOnly(now)) {
		return selectStyle.Render("▣")
	}
	if day.Count == 0 {
		return mutedStyle.Render("·")
	}
	if day.Count == 1 {
		return brandStyle.Render("▪")
	}
	return goodStyle.Render("█")
}

func renderAchievementGrid(items []achievement, width int) string {
	columns := 2
	if width < 70 {
		columns = 1
	}
	columnWidth := max(28, (width-4)/columns)
	var b strings.Builder
	for index := 0; index < len(items); index += columns {
		for column := 0; column < columns; column++ {
			if index+column >= len(items) {
				break
			}
			item := items[index+column]
			entry := achievementEntry(item, columnWidth)
			if column > 0 {
				b.WriteString("  ")
			}
			b.WriteString(entry)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func achievementEntry(item achievement, width int) string {
	progress := min(item.Progress, item.Target)
	label := fmt.Sprintf("%s %s %d/%d", "◇", item.Name, progress, item.Target)
	if item.Progress >= item.Target {
		label = "◆ " + item.Name
		return goodStyle.Render(padRight(label, width))
	}
	return mutedStyle.Render(padRight(label, width))
}

func renderJourney(job application, width int) string {
	labelWidth := min(38, max(24, width/3))
	label := truncate(job.Company+" / "+job.Role, labelWidth)
	if strings.EqualFold(job.Status, "Rejected") {
		return keyStyle.Render("  ▸ ") + inputStyle.Render(padRight(label, labelWidth)) + badStyle.Render("  PROSPECT  →  APPLIED  →  ◉ CLOSED")
	}
	phase := journeyPriority(job.Status)
	stages := []string{"PROSPECT", "APPLIED", "INTERVIEW", "OFFER"}
	var b strings.Builder
	b.WriteString(keyStyle.Render("  ▸ "))
	b.WriteString(inputStyle.Render(padRight(label, labelWidth)))
	b.WriteString("  ")
	for index, stage := range stages {
		stageNumber := index + 2
		if index > 0 {
			b.WriteString(frameStyle.Render(" → "))
		}
		switch {
		case phase > stageNumber:
			b.WriteString(goodStyle.Render("◆ " + stage))
		case phase == stageNumber:
			b.WriteString(keyStyle.Render("▸ " + stage))
		default:
			b.WriteString(mutedStyle.Render("○ " + stage))
		}
	}
	return b.String()
}

func metric(label string, value int) string {
	return systemStyle.Render(label+" ") + goodStyle.Render(fmt.Sprintf("%d", value))
}

func renderTrend(title string, buckets []chartBucket, width int) string {
	var b strings.Builder
	b.WriteString(tableHeaderStyle.Render(title))
	b.WriteString("\n")

	maxCount := 0
	for _, bucket := range buckets {
		if bucket.Count > maxCount {
			maxCount = bucket.Count
		}
	}
	barWidth := min(36, max(10, width-26))
	for _, bucket := range buckets {
		b.WriteString(systemStyle.Render("  " + padRight(bucket.Label, 9)))
		b.WriteString(frameStyle.Render(" │ "))
		if bucket.Count == 0 || maxCount == 0 {
			b.WriteString(mutedStyle.Render("·"))
		} else {
			length := max(1, (bucket.Count*barWidth+maxCount-1)/maxCount)
			b.WriteString(brandStyle.Render(strings.Repeat("█", length)))
		}
		b.WriteString(" ")
		b.WriteString(inputStyle.Render(fmt.Sprintf("%d", bucket.Count)))
		b.WriteString("\n")
	}
	return b.String()
}

func buildSourceStats(data dataFile) []sourceStat {
	bySource := make(map[string]sourceStat)
	for _, job := range data.Applications {
		if strings.EqualFold(job.Status, "Archived") {
			continue
		}
		source := sourceLabel(job.Source)
		stat := bySource[source]
		stat.Source = source
		stat.Tracked++
		if isSubmitted(job.Status) {
			stat.Submitted++
		}
		if strings.EqualFold(job.Status, "Interview") || strings.EqualFold(job.Status, "Offer") {
			stat.Advanced++
		}
		if strings.EqualFold(job.Status, "Offer") {
			stat.Offers++
		}
		bySource[source] = stat
	}

	sources := make([]sourceStat, 0, len(bySource))
	for _, stat := range bySource {
		sources = append(sources, stat)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Submitted != sources[j].Submitted {
			return sources[i].Submitted > sources[j].Submitted
		}
		if sources[i].Advanced != sources[j].Advanced {
			return sources[i].Advanced > sources[j].Advanced
		}
		return strings.ToLower(sources[i].Source) < strings.ToLower(sources[j].Source)
	})
	return sources
}

func sourceLabel(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "Unspecified"
	}
	return value
}

func renderSourceAttribution(sources []sourceStat, width int) string {
	var b strings.Builder
	b.WriteString(tableHeaderStyle.Render("SOURCE ATTRIBUTION  /  SUBMISSIONS AND OUTCOMES"))
	b.WriteString("\n")
	maxSubmitted := 0
	for _, source := range sources {
		maxSubmitted = max(maxSubmitted, source.Submitted)
	}
	nameWidth := min(24, max(16, width-54))
	barWidth := min(24, max(8, width-nameWidth-38))
	for _, source := range sources[:min(10, len(sources))] {
		bar := mutedStyle.Render("·")
		if source.Submitted > 0 && maxSubmitted > 0 {
			length := max(1, (source.Submitted*barWidth+maxSubmitted-1)/maxSubmitted)
			bar = brandStyle.Render(strings.Repeat("█", length))
		}
		b.WriteString(keyStyle.Render("  " + padRight(truncate(source.Source, nameWidth), nameWidth)))
		b.WriteString(frameStyle.Render(" │ "))
		b.WriteString(bar)
		b.WriteString(inputStyle.Render(fmt.Sprintf("  %d submitted", source.Submitted)))
		b.WriteString(systemStyle.Render(fmt.Sprintf("  ·  %d advanced", source.Advanced)))
		if source.Offers > 0 {
			b.WriteString(goodStyle.Render(fmt.Sprintf("  ·  %d offer", source.Offers)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func buildInsightStats(data dataFile, now time.Time) insightStats {
	now = dateOnly(now)
	stats := insightStats{}
	weekStart := startOfWeek(now)
	for i := 5; i >= 0; i-- {
		start := weekStart.AddDate(0, 0, -7*i)
		stats.Weekly = append(stats.Weekly, chartBucket{Label: start.Format("Jan 02"), Start: start})
	}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	for i := 5; i >= 0; i-- {
		start := monthStart.AddDate(0, -i, 0)
		stats.Monthly = append(stats.Monthly, chartBucket{Label: start.Format("Jan 06"), Start: start})
	}

	for _, job := range data.Applications {
		status := strings.ToLower(strings.TrimSpace(job.Status))
		if status == "archived" {
			continue
		}
		stats.Tracked++
		switch status {
		case "prospect":
			stats.Prospects++
		case "applied":
			stats.Applied++
		case "interview":
			stats.Interviews++
		case "offer":
			stats.Offers++
		case "rejected":
			stats.Rejected++
		}
		if !isSubmitted(job.Status) {
			continue
		}
		stats.Submitted++
		date, ok := submissionDate(job)
		if !ok {
			stats.Undated++
			continue
		}
		for i := range stats.Weekly {
			start := stats.Weekly[i].Start
			if !date.Before(start) && date.Before(start.AddDate(0, 0, 7)) {
				stats.Weekly[i].Count++
				break
			}
		}
		for i := range stats.Monthly {
			start := stats.Monthly[i].Start
			if date.Year() == start.Year() && date.Month() == start.Month() {
				stats.Monthly[i].Count++
				break
			}
		}
	}

	for _, person := range data.Contacts {
		status := strings.ToLower(strings.TrimSpace(person.Status))
		if status == "archived" {
			continue
		}
		stats.Contacts++
		switch status {
		case "sent", "replied", "meeting", "nurture":
			stats.Outreach++
		}
		if status == "replied" {
			stats.Replies++
		}
		if status == "meeting" {
			stats.Meetings++
		}
		if followup, ok := parseTrackerDate(person.NextFollowup); ok && !followup.After(now) {
			stats.FollowupDue++
		}
	}
	return stats
}

func isSubmitted(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "applied", "interview", "offer", "rejected":
		return true
	default:
		return false
	}
}

func submissionDate(job application) (time.Time, bool) {
	if !isSubmitted(job.Status) {
		return time.Time{}, false
	}
	if date, ok := parseTrackerDate(job.AppliedDate); ok {
		return date, true
	}
	return parseTrackerDate(job.Date)
}

func parseTrackerDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02", "1-2-2006", "01-02-2006", "1/2/2006", "01/02/2006", "Jan 02 2006", "Jan 2 2006"} {
		if date, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return dateOnly(date), true
		}
	}
	return time.Time{}, false
}

func startOfWeek(date time.Time) time.Time {
	offset := (int(date.Weekday()) + 6) % 7 // Monday is the first day of the week.
	return dateOnly(date).AddDate(0, 0, -offset)
}

func dateOnly(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
}

func latestBucketCount(buckets []chartBucket) int {
	if len(buckets) == 0 {
		return 0
	}
	return buckets[len(buckets)-1].Count
}

func percent(numerator, denominator int) string {
	if denominator == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(numerator)*100/float64(denominator))
}

func padRight(value string, width int) string {
	value = truncate(value, width)
	return value + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(value)))
}

func (m model) viewForm() string {
	labels := []string{"Company", "Role", "Location (e.g., Seattle, WA)", "Official URL", "Source (e.g., LinkedIn, Referral, Company site)", "Date (YYYY-MM-DD)", "Notes"}
	title := "ADD APPLICATION"
	if m.section == networkingSection {
		labels = []string{"Name", "Company", "Title", "Relationship / Context", "Profile URL", "Last Contact (YYYY-MM-DD)", "Next Follow-up (YYYY-MM-DD)", "Notes"}
		title = "ADD NETWORKING CONTACT"
	} else if m.section == tasksSection {
		labels = []string{"Task / note"}
		title = "ADD TASK NOTE"
	}
	if m.form.editing {
		title = "EDIT " + strings.TrimPrefix(title, "ADD ")
	}
	var b strings.Builder
	b.WriteString(m.header(title, "INPUT TERMINAL  /  LOCAL DATA ONLY") + "\n")
	b.WriteString(systemStyle.Render("Tab / Enter next field  ·  Ctrl+S save  ·  Esc cancel") + "\n\n")
	for i, label := range labels {
		prefix, style := "  ", mutedStyle
		if i == m.form.field {
			prefix, style = "› ", keyStyle
		}
		value := m.form.values[i]
		if i == m.form.field {
			value += "█"
		}
		b.WriteString(style.Render(prefix+label) + "\n")
		b.WriteString(inputStyle.Render("    "+value) + "\n\n")
	}
	if m.message != "" {
		b.WriteString(m.message)
	}
	return b.String()
}

func (m model) viewGoalsForm() string {
	labels := []string{
		"Weekly application goal",
		"Weekly networking-action goal",
		"Weekly tracker-action goal",
	}
	var b strings.Builder
	b.WriteString(m.header("SET WEEKLY GOALS", "MISSION CONTROL  /  YOUR LOCAL TARGETS") + "\n")
	b.WriteString(systemStyle.Render("These numbers only shape the Quest Board. They never create activity or change your records.") + "\n\n")
	for index, label := range labels {
		prefix, style := "  ", mutedStyle
		if index == m.goals.field {
			prefix, style = "› ", keyStyle
		}
		value := m.goals.values[index]
		if index == m.goals.field {
			value += "█"
		}
		b.WriteString(style.Render(prefix+label+"  (1–99)") + "\n")
		b.WriteString(inputStyle.Render("    "+value) + "\n\n")
	}
	b.WriteString(mutedStyle.Render("Tab / Enter next field  ·  Ctrl+S save  ·  Esc cancel"))
	return b.String()
}

func (m model) viewStatusPicker() string {
	var b strings.Builder
	b.WriteString(m.header("CHANGE STATUS", "WORKFLOW CONTROL"))
	b.WriteString("\n" + systemStyle.Render(m.selectedLabel()) + "\n\n")
	for i, option := range m.sectionStatuses() {
		line := "  " + option
		if i == m.statusCursor {
			b.WriteString(selectStyle.Render("› " + option))
		} else {
			b.WriteString(statusStyle(option).Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + mutedStyle.Render("↑↓ choose  Enter save  Esc cancel"))
	return b.String()
}

func (m model) viewDeletePicker() string {
	return m.header("DELETE RECORD?", "IRREVERSIBLE LOCAL ACTION") + "\n\n" +
		badStyle.Render(m.selectedLabel()) + "\n" +
		systemStyle.Render("This permanently removes it from your local JSON file.") + "\n\n" +
		keyStyle.Render("y / Enter") + mutedStyle.Render(" delete permanently   ") + keyStyle.Render("n / Esc") + mutedStyle.Render(" cancel")
}

func (m model) header(label, subtitle string) string {
	labelBlock := titleStyle.Render(" " + label + " ")
	accent := brandStyle.Render(" // ")
	width := max(18, m.width-lipgloss.Width(labelBlock)-lipgloss.Width(accent)-1)
	return labelBlock + accent + frameStyle.Render(strings.Repeat("═", width)) + "\n" + systemStyle.Render("  "+subtitle)
}

func (m model) renderMessage() string {
	if strings.Contains(m.message, "\x1b[") {
		return m.message
	}
	return goodStyle.Render("SYSTEM OK  ") + inputStyle.Render(m.message)
}

func rowMarker(selected bool) string {
	if selected {
		return keyStyle.Render("▸ ")
	}
	return "  "
}

func (m model) sectionTabs() []string {
	switch m.section {
	case applicationsSection:
		return append([]string{"All"}, appStatuses...)
	case networkingSection:
		return append([]string{"All"}, contactStatuses...)
	case insightsSection:
		return []string{"Overview", "Sources"}
	case timelineSection:
		return []string{"All", "Applications", "Networking", "Tasks"}
	case tasksSection:
		return []string{"All", "Open", "Done"}
	default:
		return []string{"Overview"}
	}
}

func (m model) sectionStatuses() []string {
	if m.section == applicationsSection {
		return appStatuses
	}
	if m.section == networkingSection {
		return contactStatuses
	}
	if m.section == tasksSection {
		return []string{"Open", "Done"}
	}
	return nil
}

func (m model) visibleIndices() []int {
	if m.section == insightsSection || m.section == timelineSection || m.section == geographySection || m.section == missionSection {
		return nil
	}
	filter := m.sectionTabs()[m.tab]
	if m.section == applicationsSection {
		indices := make([]int, 0, len(m.data.Applications))
		for i, j := range m.data.Applications {
			if filter == "All" || strings.EqualFold(j.Status, filter) {
				indices = append(indices, i)
			}
		}
		sort.SliceStable(indices, func(a, b int) bool {
			return m.data.Applications[indices[a]].Date > m.data.Applications[indices[b]].Date
		})
		return indices
	}
	if m.section == tasksSection {
		indices := make([]int, 0, len(m.data.Tasks))
		for i, task := range m.data.Tasks {
			status := taskStatus(task)
			if filter == "All" || strings.EqualFold(status, filter) {
				indices = append(indices, i)
			}
		}
		sort.SliceStable(indices, func(a, b int) bool {
			left, right := m.data.Tasks[indices[a]], m.data.Tasks[indices[b]]
			if left.Done != right.Done {
				return !left.Done
			}
			return left.UpdatedAt > right.UpdatedAt
		})
		return indices
	}
	indices := make([]int, 0, len(m.data.Contacts))
	for i, c := range m.data.Contacts {
		if filter == "All" || strings.EqualFold(c.Status, filter) {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(a, b int) bool {
		return m.data.Contacts[indices[a]].NextFollowup < m.data.Contacts[indices[b]].NextFollowup
	})
	return indices
}

func (m model) selectedApplication() (application, bool) {
	indices := m.visibleIndices()
	if m.section != applicationsSection || len(indices) == 0 || m.cursor < 0 || m.cursor >= len(indices) {
		return application{}, false
	}
	return m.data.Applications[indices[m.cursor]], true
}

func (m model) selectedContact() (contact, bool) {
	indices := m.visibleIndices()
	if m.section != networkingSection || len(indices) == 0 || m.cursor < 0 || m.cursor >= len(indices) {
		return contact{}, false
	}
	return m.data.Contacts[indices[m.cursor]], true
}

func (m model) selectedTask() (task, bool) {
	indices := m.visibleIndices()
	if m.section != tasksSection || len(indices) == 0 || m.cursor < 0 || m.cursor >= len(indices) {
		return task{}, false
	}
	return m.data.Tasks[indices[m.cursor]], true
}

func (m model) hasSelection() bool {
	return len(m.visibleIndices()) > 0 && m.cursor < len(m.visibleIndices())
}

func (m model) selectedID() int {
	if j, ok := m.selectedApplication(); ok {
		return j.ID
	}
	if c, ok := m.selectedContact(); ok {
		return c.ID
	}
	if t, ok := m.selectedTask(); ok {
		return t.ID
	}
	return 0
}

func (m model) selectedStatus() string {
	if j, ok := m.selectedApplication(); ok {
		return j.Status
	}
	if c, ok := m.selectedContact(); ok {
		return c.Status
	}
	if t, ok := m.selectedTask(); ok {
		return taskStatus(t)
	}
	return ""
}

func (m model) selectedURL() string {
	if j, ok := m.selectedApplication(); ok {
		return j.URL
	}
	if c, ok := m.selectedContact(); ok {
		return c.ProfileURL
	}
	return ""
}

func (m model) selectedLabel() string {
	if j, ok := m.selectedApplication(); ok {
		return j.Company + " — " + j.Role
	}
	if c, ok := m.selectedContact(); ok {
		return c.Name + " — " + c.Company
	}
	if t, ok := m.selectedTask(); ok {
		return taskSubject(t)
	}
	return "Selected record"
}

func (m model) count(tab string) int {
	if m.section == insightsSection || m.section == missionSection {
		return 0
	}
	if m.section == geographySection {
		return 0
	}
	if m.section == timelineSection {
		return m.activityCount(tab)
	}
	if tab == "All" {
		if m.section == applicationsSection {
			return len(m.data.Applications)
		}
		if m.section == tasksSection {
			return len(m.data.Tasks)
		}
		return len(m.data.Contacts)
	}
	n := 0
	if m.section == applicationsSection {
		for _, j := range m.data.Applications {
			if strings.EqualFold(j.Status, tab) {
				n++
			}
		}
		return n
	}
	if m.section == tasksSection {
		for _, task := range m.data.Tasks {
			if strings.EqualFold(taskStatus(task), tab) {
				n++
			}
		}
		return n
	}
	for _, c := range m.data.Contacts {
		if strings.EqualFold(c.Status, tab) {
			n++
		}
	}
	return n
}

func (m *model) addActivity(entityType string, entityID int, subject, action, detail string) {
	if m.data.NextActivityID < 1 {
		m.data.NextActivityID = maxActivityID(m.data.Activities) + 1
	}
	m.data.Activities = append(m.data.Activities, activity{
		ID:         m.data.NextActivityID,
		EntityType: entityType,
		EntityID:   entityID,
		Subject:    subject,
		Action:     action,
		Detail:     detail,
		OccurredAt: time.Now().Format(time.RFC3339),
	})
	m.data.NextActivityID++
}

func applicationSubject(job application) string {
	return strings.TrimSpace(job.Company + " — " + job.Role)
}

func contactSubject(person contact) string {
	if strings.TrimSpace(person.Company) == "" {
		return strings.TrimSpace(person.Name)
	}
	return strings.TrimSpace(person.Name + " — " + person.Company)
}

func taskSubject(item task) string {
	return truncate(item.Text, 60)
}

func taskStatus(item task) string {
	if item.Done {
		return "Done"
	}
	return "Open"
}

func loadData(path string) (dataFile, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		d := dataFile{NextApplicationID: 1, NextContactID: 1, NextTaskID: 1, NextActivityID: 1, Goals: resolvedWeeklyGoals(weeklyGoals{}), Applications: []application{}, Contacts: []contact{}, Tasks: []task{}, Activities: []activity{}}
		normalizeEasterEggs(&d)
		return d, nil
	}
	if err != nil {
		return dataFile{}, err
	}
	var d dataFile
	if err := json.Unmarshal(content, &d); err != nil {
		return dataFile{}, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if d.NextApplicationID < 1 {
		d.NextApplicationID = maxApplicationID(d.Applications) + 1
	}
	if d.NextContactID < 1 {
		d.NextContactID = maxContactID(d.Contacts) + 1
	}
	if d.NextTaskID < 1 {
		d.NextTaskID = maxTaskID(d.Tasks) + 1
	}
	if d.NextActivityID < 1 {
		d.NextActivityID = maxActivityID(d.Activities) + 1
	}
	d.Goals = resolvedWeeklyGoals(d.Goals)
	normalizeEasterEggs(&d)
	return d, nil
}

func saveData(path string, d dataFile) error {
	content, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(content, '\n'), 0o600)
}

func removeApplication(items []application, id int) []application {
	for i, item := range items {
		if item.ID == id {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func removeContact(items []contact, id int) []contact {
	for i, item := range items {
		if item.ID == id {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func removeTask(items []task, id int) []task {
	for i, item := range items {
		if item.ID == id {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		if err := exec.Command("open", url).Run(); err != nil {
			return errMsg(err)
		}
		return statusMsg("Opened URL in your browser.")
	}
}

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "Offer", "Interview", "Meeting", "Replied", "Done":
		return goodStyle
	case "Rejected", "Archived":
		return badStyle
	case "Applied", "Sent", "To Reach Out":
		return warnStyle
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#C0CAF5"))
	}
}

func truncate(value string, width int) string {
	value = strings.TrimSpace(value)
	if width < 4 || utf8.RuneCountInString(value) <= width {
		return value
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if strings.EqualFold(value, target) {
			return i
		}
	}
	return -1
}

func maxApplicationID(items []application) int {
	maxID := 0
	for _, item := range items {
		if item.ID > maxID {
			maxID = item.ID
		}
	}
	return maxID
}

func maxContactID(items []contact) int {
	maxID := 0
	for _, item := range items {
		if item.ID > maxID {
			maxID = item.ID
		}
	}
	return maxID
}

func maxTaskID(items []task) int {
	maxID := 0
	for _, item := range items {
		if item.ID > maxID {
			maxID = item.ID
		}
	}
	return maxID
}

func maxActivityID(items []activity) int {
	maxID := 0
	for _, item := range items {
		if item.ID > maxID {
			maxID = item.ID
		}
	}
	return maxID
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
