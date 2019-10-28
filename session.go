package main

import (
	"log"
	"sync"
	"time"
)

var Timeout = time.Second * 5

type Session struct {
	gateways   map[string]int
	token      string
	time       time.Time
	next, prev *Session
}

var latestSess, oldestSess *Session

var sessions = map[string]*Session{}

var sessMutex sync.Mutex

func CreateSession(token string) *Session {

	oldSess := GetSession(token)
	if oldSess != nil {
		return oldSess
	}

	sessMutex.Lock()

	deadline := time.Now().Add(-Timeout)
	dropCount := 0

	for oldestSess != nil && oldestSess.time.Before(deadline) {
		delete(sessions, oldestSess.token)
		dropCount++
		oldestSess = oldestSess.next
	}

	sess := &Session{
		time:  time.Now(),
		token: token,
	}
	sessions[token] = sess

	if oldestSess == nil {
		oldestSess = sess
	} else {
		oldestSess.prev = nil
		sess.prev = latestSess
		latestSess.next = sess
	}
	latestSess = sess
	count := len(sessions)
	sessMutex.Unlock()
	log.Printf("[SESS ] Added 1, Removed %d, Total %d", dropCount, count)
	return sess
}

func GetSession(token string) *Session {
	sessMutex.Lock()
	sess := sessions[token]
	if sess != nil {
		sess.time = time.Now()
		if latestSess != sess {
			prev := sess.prev
			if prev != nil {
				prev.next = sess.next
			}
			next := sess.next
			if next != nil {
				next.prev = sess.prev
			}
			sess.prev = latestSess
			latestSess.next = sess
			latestSess = sess
		}
	}
	sessMutex.Unlock()
	return sess
}
