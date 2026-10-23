package main

import (
	"fmt"
	"os"
)

var stderr = os.Stderr

type scenario struct {
	Name         string        `json:"name"`
	Pass         bool          `json:"pass"`
	Error        string        `json:"error,omitempty"`
	Work         *work         `json:"work,omitempty"`
	Observations []observation `json:"observations,omitempty"`
}

func (h *harness) verify(r *report) error {
	check := func(name string, fn func(*scenario) error) {
		s := scenario{Name: name}
		e := fn(&s)
		s.Pass = e == nil
		if e != nil {
			s.Error = e.Error()
			r.Failures = append(r.Failures, name+": "+e.Error())
			fmt.Fprintln(stderr, "FAIL", name, e)
		} else {
			fmt.Fprintln(stderr, "PASS", name)
		}
		r.Scenarios = append(r.Scenarios, s)
	}
	complete := func(o observation, provider string) error {
		if o.Error != "" || !o.Complete || o.Provider != provider {
			return fmt.Errorf("unexpected completion: %+v", o)
		}
		return nil
	}
	check("baseline JSON and streaming", func(s *scenario) error {
		if e := h.recover(); e != nil {
			return e
		}
		a, e := h.snapshot()
		if e != nil {
			return e
		}
		for _, stream := range []bool{false, true} {
			o := h.chat(h.gateway, "demo-key", false, stream, "baseline")
			s.Observations = append(s.Observations, o)
			if e = complete(o, "primary"); e != nil {
				return e
			}
		}
		b, e := h.snapshot()
		if e != nil {
			return e
		}
		w := delta(a, b)
		s.Work = &w
		if w.Requests != 2 || w.Attempts != 2 || w.Fake["primary"] != 2 || w.Fake["backup"] != 0 {
			return fmt.Errorf("baseline work: %+v", w)
		}
		return nil
	})
	check("primary 503 fallback, circuit opening", func(s *scenario) error {
		small := *h
		small.o.requests = 3
		small.cleanupErrors = nil
		p, e := small.outage(0)
		h.cleanupErrors = append(h.cleanupErrors, small.cleanupErrors...)
		s.Work = p.Work
		s.Observations = p.Observations
		if e != nil {
			return e
		}
		if len(p.Failures) > 0 {
			return fmt.Errorf("%v", p.Failures)
		}
		return nil
	})
	check("real recovery probe", func(s *scenario) error { return h.recover() })
	check("slow first chunk fallback", func(s *scenario) error {
		if e := h.control("primary", 200, int(h.o.firstTimeout.Milliseconds())+100, 2, -1); e != nil {
			return e
		}
		defer h.cleanupHealthy()
		a, e := h.snapshot()
		if e != nil {
			return e
		}
		o := h.chat(h.gateway, "demo-key", false, true, "slow first chunk")
		s.Observations = append(s.Observations, o)
		if e = complete(o, "backup"); e != nil {
			return e
		}
		b, e := h.waitForPrimaryCancellation(a)
		if e != nil {
			return e
		}
		w := delta(a, b)
		s.Work = &w
		if w.Attempts != 2 || w.Fake["primary"] != 1 || w.Fake["backup"] != 1 || w.Canceled["primary"] != 1 || w.Completed["primary"] != 0 {
			return fmt.Errorf("slow fallback work: %+v", w)
		}
		return nil
	})
	check("midstream interruption never falls back", func(s *scenario) error {
		if e := h.recover(); e != nil {
			return e
		}
		if e := h.control("primary", 200, 0, 2, 2); e != nil {
			return e
		}
		defer h.cleanupHealthy()
		a, e := h.snapshot()
		if e != nil {
			return e
		}
		o := h.chat(h.gateway, "demo-key", false, true, "cut stream")
		s.Observations = append(s.Observations, o)
		b, e := h.snapshot()
		if e != nil {
			return e
		}
		w := delta(a, b)
		s.Work = &w
		if o.Status != 200 || o.Complete || o.Error == "" || o.Text == "" || o.Text == h.expected || w.Attempts != 1 || w.Fake["primary"] != 1 || w.Fake["backup"] != 0 {
			return fmt.Errorf("partial response or work invalid: %+v %+v", o, w)
		}
		return nil
	})
	check("eligible cache second hit has no provider work", func(s *scenario) error {
		if e := h.recover(); e != nil {
			return e
		}
		prompt := "verify cache " + r.Timestamp
		first := h.chat(h.gateway, "demo-key", true, true, prompt)
		s.Observations = append(s.Observations, first)
		if e := complete(first, "primary"); e != nil {
			return e
		}
		if first.CacheHit {
			return fmt.Errorf("cache prime already hit")
		}
		a, e := h.snapshot()
		if e != nil {
			return e
		}
		o := h.chat(h.gateway, "demo-key", true, true, prompt)
		s.Observations = append(s.Observations, o)
		if e = complete(o, "primary"); e != nil {
			return e
		}
		b, e := h.snapshot()
		if e != nil {
			return e
		}
		w := delta(a, b)
		s.Work = &w
		if !o.CacheHit || o.Cost != "0" || w.Requests != 1 || w.Attempts != 0 || w.fakeTotal() != 0 {
			return fmt.Errorf("cache metadata/work invalid: %+v %+v", o, w)
		}
		return nil
	})
	check("client rate 429", func(s *scenario) error {
		a := h.chat(h.gateway, "rate-key", false, false, "rate first")
		s.Observations = append(s.Observations, a)
		if e := complete(a, "primary"); e != nil {
			return e
		}
		before, e := h.snapshot()
		if e != nil {
			return e
		}
		o := h.chat(h.gateway, "rate-key", false, false, "rate second")
		s.Observations = append(s.Observations, o)
		after, e := h.snapshot()
		if e != nil {
			return e
		}
		w := delta(before, after)
		s.Work = &w
		if o.Status != 429 || o.Complete || o.ErrorCode == "" || w.Attempts != 0 || w.fakeTotal() != 0 {
			return fmt.Errorf("wanted 429: %+v", o)
		}
		return nil
	})
	for _, tc := range []struct {
		name, key string
		status    int
	}{{"client budget 402", "budget-key", 402}, {"unauthorized 401", "invalid-key", 401}, {"missing authorization 401", "", 401}} {
		tc := tc
		check(tc.name, func(s *scenario) error {
			a, e := h.snapshot()
			if e != nil {
				return e
			}
			o := h.chat(h.gateway, tc.key, false, false, "admission")
			s.Observations = append(s.Observations, o)
			b, e := h.snapshot()
			if e != nil {
				return e
			}
			w := delta(a, b)
			s.Work = &w
			if o.Status != tc.status || o.Complete || o.ErrorCode == "" || w.Attempts != 0 || w.fakeTotal() != 0 {
				return fmt.Errorf("wanted %d without upstream work: %+v %+v", tc.status, o, w)
			}
			return nil
		})
	}
	check("stats requests and attempts consistency", func(s *scenario) error {
		snap, e := h.snapshot()
		if e != nil {
			return e
		}
		t := snap.Totals
		if t.Active != 0 || t.Requests != t.Succeeded+t.Failed+t.Rejected {
			return fmt.Errorf("totals inconsistent: %+v", t)
		}
		var records struct {
			Items []struct {
				Status   string
				Attempts []struct{ Provider string }
			}
		}
		if e = h.api("GET", "/api/v1/requests?limit=1000", nil, &records); e != nil {
			return e
		}
		var attempts int64
		for _, v := range records.Items {
			attempts += int64(len(v.Attempts))
		}
		if int64(len(records.Items)) != t.Requests || attempts != t.ProviderAttempts {
			return fmt.Errorf("record counts requests=%d attempts=%d; totals requests=%d attempts=%d (external server must be fresh)", len(records.Items), attempts, t.Requests, t.ProviderAttempts)
		}
		return nil
	})
	return nil
}
