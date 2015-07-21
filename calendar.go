package calendar

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/gorilla/mux"
)

const (
	tmLabelShort = "2006-01-02"
	tmLabelLong  = "2006-01-02T15:04:05-07:00"
	tmShortTime  = "T00:00:00-04:00" // @TODO hardcoded timezone offset is no good. Fix.
)

type jEvent struct {
	ID          string                    `json:"id"`
	Attendees   []*calendar.EventAttendee `json:"attendees"`
	AllDay      bool                      `json:"allDayEvent"`
	ColorBgd    string                    `json:"color"`
	Date        string                    `json:"date"`
	Description string                    `json:"description"`
	Location    string                    `json:"location"`
	Summary     string                    `json:"summary"`
}

func (s *jEvent) setAllDay(flag bool) {
	s.AllDay = flag
}

type newEvent struct {
	Color       string
	Date        string
	Description string
	Location    string
	Summary     string
}

var calS CalService
var srv *calendar.Service
var loc *time.Location

func init() {
	loc, _ = time.LoadLocation("Local")
	srv = calS.New()
}

// MonthEvents method fetches events for specified month with some overlap
func MonthEvents(w http.ResponseWriter, r *http.Request) {
	// Restrict method to get only
	if r.Method != "GET" {
		respondErr(w, r, http.StatusMethodNotAllowed, "invalid method: "+r.Method)
		return
	}

	// the gorilla/mux package allows us to extract vars from path
	vars := mux.Vars(r)
	if vars["date"] == "" { // can't see getting routed here without this var, but...
		respondErr(w, r, http.StatusForbidden, "invalid request, missing date")
		return
	}
	dtVar := vars["date"]

	// Set date string to create a time object
	dtStr := dtVar[0:4] + "-" + dtVar[4:] + "-01"
	tm, _ := time.ParseInLocation(tmLabelShort, dtStr, loc)
	// To get some overlap ensuring that the days displayed on a month calendar
	// also display their respective events, we grab a week before and 2 after
	startDte := tm.AddDate(0, 0, -7).Format(time.RFC3339)
	endDte := tm.AddDate(0, 1, 14).Format(time.RFC3339)
	// Fetch colors so we can display
	clrs, err := srv.Colors.Get().Do()

	// Fetch events
	events, err := srv.Events.List("primary").
		ShowDeleted(false).
		SingleEvents(true).
		Fields("items(id,attendees,colorId,creator,description,updated,start,summary)").
		TimeMin(startDte).
		TimeMax(endDte).
		OrderBy("startTime").
		Do()
	if err != nil {
		log.Fatalf("Unable to retrieve next ten of the user's events. %v", err)
		respondErr(w, r, http.StatusNoContent, "Unable to retrieve user's events")
	}
	res := []*jEvent{}
	if len(events.Items) > 0 {
		for _, i := range events.Items {
			ev := &jEvent{}
			res1, _ := json.Marshal(i)
			// Set color
			ev.ColorBgd = clrs.Event[i.ColorId].Background

			// If the DateTime is an empty string the Event is an all-day Event.
			// Formatting date the same with allDayEvent flag allows end-user
			// the option of how to handle
			if i.Start.DateTime != "" {
				ts, _ := time.Parse(tmLabelLong, i.Start.DateTime)
				ev.Date = ts.Format(time.RFC3339)
				ev.setAllDay(false)
			} else {
				// To keep things simple for the js date interpretation, we're formatting all day event
				// dates the same as a DateTime (above)
				ts, _ := time.Parse(tmLabelLong, i.Start.Date+tmShortTime)
				ev.Date = ts.Format(time.RFC3339)
				ev.setAllDay(true)
			}
			json.Unmarshal(res1, &ev)
			res = append(res, ev)
		}
	}
	respond(w, r, http.StatusOK, res)
}

// Event method - Redirect event request to appropriate method
func Event(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		fetchEvent(w, r)
	case "POST":
		createEvent(w, r)
	case "PATCH":
		updateEvent(w, r)
	case "DELETE":
		deleteEvent(w, r)
	}
}

func fetchEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	eID := vars["id"]
	ev, err := srv.Events.Get("primary", eID).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve event. %v", err)
		respondErr(w, r, http.StatusNotFound)
		return
	}
	respond(w, r, http.StatusOK, ev)
}

func createEvent(w http.ResponseWriter, r *http.Request) {
	// https://github.com/google/google-api-go-client/blob/master/calendar/v3/calendar-gen.go
	// line: 476 - Event struct
	// line: 3512 - method id "calendar.events.insert"
	var newEv newEvent
	err := json.NewDecoder(r.Body).Decode(&newEv)
	if err != nil {
		log.Println(err.Error())
	}
	r.Body.Close()

	evt := assembleEvent(&newEv)
	ev, err := srv.Events.Insert("primary", evt).Fields("id").Do()
	if err != nil {
		log.Println(err.Error())
		respondErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, r, http.StatusCreated, ev)
}

func updateEvent(w http.ResponseWriter, r *http.Request) {
	var pEv newEvent
	// the gorilla/mux package allows us to extract vars from path
	vars := mux.Vars(r)
	if vars["id"] == "" {
		respondErr(w, r, http.StatusForbidden, "invalid request, missing event id")
		return
	}
	eID := vars["id"]

	// extract submitted event from request body and decode to newEvent struct
	err := json.NewDecoder(r.Body).Decode(&pEv)
	if err != nil {
		log.Println(err.Error())
	}
	r.Body.Close()

	// Extract data from newEvent to populate the calendar.Event struct
	evt := assembleEvent(&pEv)
	ev, err := srv.Events.Patch("primary", eID, evt).Fields("id").Do()
	if err != nil {
		log.Println(err.Error())
		respondErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, r, http.StatusOK, ev)
}

func deleteEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if vars["id"] == "" {
		respondErr(w, r, http.StatusForbidden, "invalid request, missing event id")
		return
	}

	err := srv.Events.Delete("primary", vars["id"]).Do()
	if err != nil {
		log.Println(err.Error())
		respondErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, r, http.StatusOK, true)
}

// Helper method to assemble event data
func assembleEvent(s *newEvent) *calendar.Event {
	evt := &calendar.Event{}
	if s.Date != "" {
		evt.Start = &calendar.EventDateTime{Date: s.Date}
		evt.End = &calendar.EventDateTime{Date: s.Date}
	}
	if s.Color != "" {
		evt.ColorId = s.Color
	}
	if s.Description != "" {
		evt.Description = s.Description
	}
	if s.Location != "" {
		evt.Location = s.Location
	}
	if s.Summary != "" {
		evt.Summary = s.Summary
	}
	return evt
}
