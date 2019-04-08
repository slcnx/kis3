package main

import (
	"database/sql"
	"fmt"
	"github.com/gobuffalo/packr/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mssola/user_agent"
	"github.com/rubenv/sql-migrate"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Database struct {
	sqlDB *sql.DB
}

func initDatabase() (database *Database, e error) {
	database = &Database{}
	if _, err := os.Stat(appConfig.dbPath); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(appConfig.dbPath), os.ModePerm)
	}
	database.sqlDB, e = sql.Open("sqlite3", appConfig.dbPath)
	if e != nil {
		return
	}
	e = migrateDatabase(database.sqlDB)
	return
}

func migrateDatabase(database *sql.DB) (e error) {
	migrations := &migrate.PackrMigrationSource{
		Box: packr.New("migrations", "migrations"),
	}
	_, e = migrate.Exec(database, "sqlite3", migrations, migrate.Up)
	return
}

// Tracking

func (db *Database) trackView(urlString string, ref string, ua string) {
	if len(urlString) == 0 {
		// Don't track empty urls
		return
	}
	if ref != "" {
		// Clean referrer and just keep the hostname for more privacy
		parsedRef, _ := url.Parse(ref)
		ref = parsedRef.Hostname()
	}
	if ua != "" {
		// Parse Useragent
		uaName, uaVersion := user_agent.New(ua).Browser()
		ua = uaName + " " + uaVersion
	}
	_, e := db.sqlDB.Exec("insert into views(url, ref, useragent) values(:url, :ref, :ua)", sql.Named("url", urlString), sql.Named("ref", ref), sql.Named("ua", ua))
	if e != nil {
		fmt.Println("Inserting into DB failed:", e)
	}
}

// Requesting

type View int

const (
	PAGES View = iota + 1
	REFERRERS
	USERAGENTS
	HOURS
	DAYS
	WEEKS
	MONTHS
)

type ViewsRequest struct {
	view View
	from string
	to   string
	url  string
	ref  string
	ua   string
}

type RequestResultRow struct {
	First  string `json:"first"`
	Second int    `json:"second"`
}

func (db *Database) request(request *ViewsRequest) (resultRows []*RequestResultRow, e error) {
	filterString, parameters := request.buildFilter()
	// Fix to use array as varargs
	namedArgs := make([]interface{}, len(parameters))
	for i, v := range parameters {
		namedArgs[i] = v
	}
	// Query
	statement := request.buildStatement(filterString)
	rows, e := db.sqlDB.Query(statement, namedArgs...)
	if e != nil {
		return
	} else {
		resultRows = []*RequestResultRow{}
		for rows.Next() {
			var first string
			var second int
			e = rows.Scan(&first, &second)
			if e != nil {
				_ = rows.Close()
				return
			}
			resultRows = append(resultRows, &RequestResultRow{
				First:  first,
				Second: second,
			})
		}
		return
	}
}

func (request *ViewsRequest) buildStatement(filters string) (statement string) {
	if len(filters) > 0 {
		filters = " where " + filters + " "
	} else {
		filters = " "
	}
	switch request.view {
	case PAGES:
		statement = "SELECT url as first, count(*) as second from views" + filters + "group by url;"
		return
	case REFERRERS:
		statement = "SELECT ref as first, count(*) as second from views" + filters + "group by ref;"
		return
	case USERAGENTS:
		statement = "SELECT useragent as first, count(*) as second from views" + filters + "group by useragent;"
		return
	case HOURS, DAYS, WEEKS, MONTHS:
		format := ""
		switch request.view {
		case HOURS:
			format = "%Y-%m-%d %H"
		case DAYS:
			format = "%Y-%m-%d"
		case WEEKS:
			format = "%Y-%W"
		case MONTHS:
			format = "%Y-%m"
		}
		statement = "SELECT strftime('" + format + "', time, 'localtime') as first, count(*) as second from views" + filters + "group by first;"
	}
	return
}

// Request filters

func (request *ViewsRequest) buildFilter() (filters string, parameters []sql.NamedArg) {
	parameters = []sql.NamedArg{}
	var allFilters []string
	for _, filter := range []string{
		request.buildDateTimeFilter(&parameters),
		request.buildUrlFilter(&parameters),
		request.buildRefFilter(&parameters),
		request.buildUseragentFilter(&parameters),
	} {
		if len(filter) > 0 {
			allFilters = append(allFilters, filter)
		}
	}
	filters = strings.Join(allFilters, " and ")
	return
}

func (request *ViewsRequest) buildDateTimeFilter(namedArg *[]sql.NamedArg) (dateTimeFilter string) {
	if len(request.from) > 0 && len(request.to) > 0 {
		*namedArg = append(*namedArg, sql.Named("from", request.from))
		*namedArg = append(*namedArg, sql.Named("to", request.to))
		dateTimeFilter = "datetime(time, 'localtime') between :from and :to"
		return
	} else if len(request.from) > 0 {
		*namedArg = append(*namedArg, sql.Named("from", request.from))
		dateTimeFilter = "datetime(time, 'localtime') >= :from"
		return
	} else if len(request.to) > 0 {
		*namedArg = append(*namedArg, sql.Named("to", request.to))
		dateTimeFilter = "datetime(time, 'localtime') <= :to"
	}
	return
}

func (request *ViewsRequest) buildUrlFilter(namedArg *[]sql.NamedArg) (urlFilter string) {
	if len(request.url) > 0 {
		*namedArg = append(*namedArg, sql.Named("url", "%"+request.url+"%"))
		urlFilter = "url like :url"
	}
	return
}

func (request *ViewsRequest) buildRefFilter(namedArg *[]sql.NamedArg) (refFilter string) {
	if len(request.ref) > 0 {
		*namedArg = append(*namedArg, sql.Named("ref", "%"+request.ref+"%"))
		refFilter = "ref like :ref"
	}
	return
}

func (request *ViewsRequest) buildUseragentFilter(namedArg *[]sql.NamedArg) (refFilter string) {
	if len(request.ua) > 0 {
		*namedArg = append(*namedArg, sql.Named("ua", "%"+request.ua+"%"))
		refFilter = "useragent like :ua"
	}
	return
}
