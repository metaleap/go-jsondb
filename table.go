package fsdb

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/metaleap/go-util/fs"
	"github.com/metaleap/go-util/run"
)

type table struct {
	urun.MutexIf
	conn           *conn
	lastLoad       time.Time
	name, filePath string
	recs           M
}

func (me *table) fetch(where M) (recs map[string]M, err error) {
	var rec M
	recs = map[string]M{}
	// fast map[id] pre-fetches if where has id query:
	if idQuery := interfaces(where[IdField]); len(idQuery) > 0 {
		var ok bool
		var str string
		for _, id := range idQuery {
			if str, ok = id.(string); ok {
				if rec = m(me.recs[str]); rec != nil && rec.Match("", where, StrCmp) {
					recs[str] = rec
				}
			}
		}
	} else {
		for rid, rix := range me.recs {
			if rec = m(rix); rec != nil {
				if rec.Match(rid, where, StrCmp) {
					recs[rid] = rec
				}
			}
		}
	}
	return
}

func (me *table) reload(lazy bool) (err error) {
	var fi os.FileInfo
	defer me.UnlockIf(me.LockIf(me.shouldLock()))
	if fi, err = os.Stat(me.filePath); err == nil && ((!lazy) || me.recs == nil || me.lastLoad.UnixNano() == 0 || (me.conn.tx == nil && fi.ModTime().UnixNano() > me.lastLoad.UnixNano())) {
		var raw []byte
		if raw, err = ioutil.ReadFile(me.filePath); err == nil {
			recs := M{}
			if err = me.conn.drv.unmarshal(raw, &recs); err == nil {
				me.recs, me.lastLoad = recs, time.Now()
			}
		}
	}
	return
}

func (me *table) delete(recIDs []string) (res *result, err error) {
	var (
		num int64
		ok  bool
	)
	if err = me.reload(true); err == nil && len(recIDs) > 0 {
		defer me.UnlockIf(me.LockIf(me.shouldLock()))
		for _, rid := range recIDs {
			if _, ok = me.recs[rid]; ok {
				delete(me.recs, rid)
				num++
			}
		}
		if num > 0 {
			err = me.persist()
		}
	}
	if err == nil {
		res = &result{AffectedRows: num}
	}
	return
}

func (me *table) insert(rec M) (res *result, err error) {
	if rec == nil {
		err = errf("Cannot insert nil")
	} else if err = me.reload(true); err == nil {
		defer me.UnlockIf(me.LockIf(me.shouldLock()))
		id := int64(len(me.recs))
		sid := strf("%v", id)
		if _, ok := me.recs[sid]; ok {
			err = errf("Cannot insert: duplicate record ID")
		} else {
			me.recs[sid] = rec
			if err = me.persist(); err == nil {
				res = &result{AffectedRows: 1, InsertedLast: id}
			} else {
				delete(me.recs, sid)
			}
		}
	}
	return
}

func (me *table) persist() (err error) {
	if me.conn.tx == nil {
		var raw []byte
		if raw, err = me.conn.drv.marshal(me.recs); err == nil {
			if err = ufs.WriteBinaryFile(me.filePath, raw); err == nil {
				me.lastLoad = time.Now()
			}
		}
	} else {
		me.conn.tx.tables[me] = true
	}
	return
}

func (me *table) shouldLock() bool {
	return me.conn.drv.ConnectionCaching()
}
