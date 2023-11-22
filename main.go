package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli"

	"log"
	"math/rand"

	_ "github.com/lib/pq"
)

func main() {
	const name = "Postgres User Manager"
	const usage = "Create a database and user, or delete them."

	app := cli.NewApp()
	app.Name = name
	app.Usage = usage
	app.Version = "0.0.1"

	app.Commands = []cli.Command{
		createCommand,
		deleteCommand,
	}

	err := app.Run(os.Args)
	if err != nil {
		panic(err)
	}
}

var createCommand = cli.Command{
	Name:  "create",
	Usage: "Create database and user.",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "prod-only, p",
			Usage: "Create database only production",
		},
	},
	Action: func(ctx *cli.Context) error {
		err := create(ctx.Bool("prod-only"))
		return err
	},
}
var deleteCommand = cli.Command{
	Name:  "delete",
	Usage: "Delete database and user.",
	Action: func(ctx *cli.Context) error {
		database := ctx.Args().Get(0)
		err := delete(database)
		return err
	},
}

func outputCreate(user string, database string, password string, conStr string, prodOnly bool) {
	pterm.DefaultSection.WithLevel(0).WithTopPadding(0).Println("Password And User Successfully Created!")
	pterm.DefaultBasicText.Println(fmt.Sprintf("user:     %s\npassword: %s", pterm.LightMagenta(user), pterm.Red(password)))
	if prodOnly {
		pterm.DefaultBasicText.Println(fmt.Sprintf("database(prod): %s", pterm.Cyan(database)))
	} else {
		pterm.DefaultBasicText.Println(fmt.Sprintf("database(prod): %s\ndatabase(dev):  %s", pterm.Cyan(database), pterm.Cyan(database+"_dev")))
	}
	pterm.DefaultSection.WithTopPadding(0).WithLevel(0).Println("Connection String:")
	if prodOnly {
		pterm.DefaultBasicText.Println(fmt.Sprintf("prod: %s", pterm.Cyan(conStr)))
	} else {
		pterm.DefaultBasicText.Println(fmt.Sprintf("prod: %s\ndev:  %s", pterm.LightGreen(conStr), pterm.LightGreen(conStr+"_dev")))
	}

}

func outputDelete(user string, database string, prodOnly bool) {
	pterm.DefaultSection.WithLevel(0).WithTopPadding(0).Println("Password And User Successfully Deleted!")
	pterm.DefaultBasicText.Println(fmt.Sprintf("user: %s", pterm.LightMagenta(user)))
	if prodOnly {
		pterm.DefaultBasicText.Println(fmt.Sprintf("database(prod): %s", pterm.Cyan(database)))
	} else {
		pterm.DefaultBasicText.Println(fmt.Sprintf("database(prod): %s\ndatabase(dev):  %s", pterm.Cyan(database), pterm.Cyan(database+"_dev")))
	}
}

func openDB() *sql.DB {
	DATABASE_URL := os.Getenv("DATABASE_URL")

	db, err := sql.Open("postgres", DATABASE_URL)
	if err != nil {
		panic(err)
	}
	return db
}

func create(prodOnly bool) error {
	db := openDB()
	database, user, password, conStr := DatabaseUserPassword()
	err := createUser(db, user, password)
	if err != nil {
		return err
	}
	err = createDatabase(db, database, user)
	if err != nil {
		return err
	}
	if !prodOnly {
		err = createDatabase(db, database+"_dev", user)
		if err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	err = updateGrant(tx, database, user, prodOnly)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
		return err
	}
	db.Close()
	outputCreate(user, database, password, conStr, prodOnly)
	return nil
}

func delete(database string) (err error) {
	db := openDB()
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	if databaseExists := databaseExists(tx, database); !databaseExists {
		pterm.Error.Printf("Database '%s' not found.\n", database)
		tx.Commit()
		return nil
	}
	prodOnly := false
	if databaseExists := databaseExists(tx, database+"_dev"); !databaseExists {
		prodOnly = true
	}
	user, err := getUser(tx, database)
	tx.Commit()
	if user == "" {
		pterm.Error.Printf("Cannot find the user who is the owner of database '%s'.", database)
		return nil
	}
	if err != nil {
		return err
	}
	err = deleteDatabase(db, database)
	if err != nil {
		return err
	}
	if !prodOnly {
		err = deleteDatabase(db, database+"_dev")
		if err != nil {
			return err
		}
	}
	tx, err = db.Begin()
	if err != nil {
		panic(err)
	}
	err = revokeUser(tx, user)
	if err != nil {
		tx.Rollback()
		return err
	}
	err = deleteUser(tx, user)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
		return err
	}
	db.Close()
	outputDelete(user, database, prodOnly)
	return nil
}

func databaseExists(tx *sql.Tx, database string) bool {
	queryrow := fmt.Sprintf("SELECT datname FROM pg_catalog.pg_database WHERE lower(datname) = lower('%s');", database)
	var dbname string
	err := tx.QueryRow(queryrow).Scan(&dbname)
	if err != nil {
		return false
	}
	return true
}

func createDatabase(db *sql.DB, database string, user string) error {
	createDb := fmt.Sprintf(`CREATE DATABASE %s OWNER '%s';`, database, user)
	_, err := db.Exec(createDb)
	return err
}

func createUser(db *sql.DB, user string, password string) error {
	createuser := fmt.Sprintf(`CREATE USER %s WITH PASSWORD '%s' CREATEDB CREATEROLE;`, user, password)
	_, err := db.Exec(createuser)
	return err
}

func getUser(tx *sql.Tx, database string) (string, error) {
	groupexists := fmt.Sprintf(`SELECT pg_catalog.pg_get_userbyid(d.datdba) FROM pg_catalog.pg_database d WHERE d.datname = '%s';`, database)
	var user string
	err := tx.QueryRow(groupexists).Scan(&user)
	if err != nil {
		return "", err
	}
	return user, nil
}

func updateGrant(tx *sql.Tx, database string, user string, prodOnly bool) error {
	if !prodOnly {
		dev := database + "_dev"
		databases := []string{database, dev}
		database = strings.Join(databases, ",")
	}
	revokeconnect := fmt.Sprintf(`REVOKE CONNECT ON DATABASE %s FROM PUBLIC;`, database)
	_, err := tx.Exec(revokeconnect)
	if err != nil {
		return err
	}
	grantdatabase := fmt.Sprintf(`GRANT ALL ON DATABASE %s TO %s;`, database, user)
	_, err = tx.Exec(grantdatabase)
	if err != nil {
		return err
	}
	_, err = tx.Exec("REVOKE ALL ON SCHEMA public FROM PUBLIC;")
	if err != nil {
		return err
	}
	grantschema := fmt.Sprintf(`GRANT ALL ON SCHEMA public TO %s;`, user)
	_, err = tx.Exec(grantschema)
	if err != nil {
		return err
	}
	return nil
}

func deleteDatabase(db *sql.DB, database string) error {
	dropdatabase := fmt.Sprintf(`DROP DATABASE %s;`, database)
	_, err := db.Exec(dropdatabase)
	return err
}

func revokeUser(tx *sql.Tx, user string) error {
	revokeuser := fmt.Sprintf(`REVOKE ALL ON SCHEMA public FROM %s`, user)
	_, err := tx.Exec(revokeuser)
	return err
}

func deleteUser(tx *sql.Tx, user string) error {
	deleteuser := fmt.Sprintf(`DROP USER %s;`, user)
	_, err := tx.Exec(deleteuser)
	return err
}

func randString(digit uint32) string {
	var alphabet = []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, digit-1)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

func createPassword(digit uint32) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	b := make([]rune, digit-1)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

func DatabaseUserPassword() (string, string, string, string) {
	database := randString(12)
	user := randString(12)
	password := createPassword(32)
	conStr := fmt.Sprintf("postgresql://%s:%s@db.taigawada.work:5432/%s", user, password, database)

	return database, user, password, conStr
}
