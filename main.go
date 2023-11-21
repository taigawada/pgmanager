package main

import (
	"database/sql"
	"fmt"
	"os"

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
	Usage: "create database and user",
	Action: func(ctx *cli.Context) error {
		err := create()
		return err
	},
}
var deleteCommand = cli.Command{
	Name:  "delete",
	Usage: "delete database and user",
	Action: func(ctx *cli.Context) error {
		database := ctx.Args().Get(0)
		err, notfound := Delete(database)
		if notfound == true {
			fmt.Printf("User in database '%s' is not found.\n", database)
			return nil
		}
		return err
	},
}

func openDB() *sql.DB {
	DATABASE_URL := os.Getenv("DATABASE_URL")

	db, err := sql.Open("postgres", DATABASE_URL)
	if err != nil {
		panic(err)
	}
	return db
}

func create() error {
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
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	err = updateGrant(tx, database, user)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
		return err
	}
	fmt.Printf("connect uri:\n%s\n", conStr)
	db.Close()
	return nil
}

func Delete(database string) (err error, notfound bool) {
	db := openDB()
	user, err := getUser(db, database)
	if user == "" {
		return nil, true
	}
	if err != nil {
		return err, false
	}
	err = deleteDatabase(db, database)
	if err != nil {
		return err, false
	}
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	err = revokeUser(tx, user)
	if err != nil {
		tx.Rollback()
		return err, false
	}
	err = deleteUser(tx, user)
	if err != nil {
		tx.Rollback()
		return err, false
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
		return err, false
	}
	db.Close()
	return nil, false
}

func createDatabase(db *sql.DB, database string, user string) error {
	createDb := fmt.Sprintf(`CREATE DATABASE %s OWNER '%s';`, database, user)
	_, err := db.Exec(createDb)
	return err
}

func createUser(db *sql.DB, user string, password string) error {
	createuser := fmt.Sprintf(`CREATE USER %s WITH PASSWORD '%s' LOGIN;`, user, password)
	_, err := db.Exec(createuser)
	return err
}

func getUser(db *sql.DB, database string) (string, error) {
	groupexists := fmt.Sprintf(`SELECT  pg_catalog.pg_get_userbyid(d.datdba) FROM pg_catalog.pg_database d WHERE d.datname = '%s';`, database)
	var user string
	err := db.QueryRow(groupexists).Scan(&user)
	if err != nil {
		return "", err
	}
	return user, nil
}

func updateGrant(tx *sql.Tx, database string, user string) error {
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
