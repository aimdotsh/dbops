package actionpolicy

import (
	"fmt"
	"strings"
)

type Risk string

const (
	R0 Risk = "R0"
	R1 Risk = "R1"
	R2 Risk = "R2"
	R3 Risk = "R3"
	R4 Risk = "R4"
)

type Policy struct {
	Action               string
	Risk                 Risk
	ConfirmationRequired bool
}

var policies = map[string]Policy{
	"host.info":                   {Action: "host.info", Risk: R0},
	"host.disk.list":              {Action: "host.disk.list", Risk: R0},
	"host.port.check":             {Action: "host.port.check", Risk: R0},
	"host.directory.check":        {Action: "host.directory.check", Risk: R0},
	"mysql.metrics":               {Action: "mysql.metrics", Risk: R0},
	"mysql.precheck":              {Action: "mysql.precheck", Risk: R0},
	"mysql.install":               {Action: "mysql.install", Risk: R2},
	"mysql.start":                 {Action: "mysql.start", Risk: R1},
	"mysql.stop":                  {Action: "mysql.stop", Risk: R3, ConfirmationRequired: true},
	"mysql.restart":               {Action: "mysql.restart", Risk: R3, ConfirmationRequired: true},
	"mysql.replication.precheck":  {Action: "mysql.replication.precheck", Risk: R0},
	"mysql.replication.create":    {Action: "mysql.replication.create", Risk: R3, ConfirmationRequired: true},
	"mysql.replication.status":    {Action: "mysql.replication.status", Risk: R0},
	"mysql.backup":                {Action: "mysql.backup", Risk: R1},
	"mysql.xtrabackup.backup":     {Action: "mysql.xtrabackup.backup", Risk: R1},
	"mysql.restore":               {Action: "mysql.restore", Risk: R4, ConfirmationRequired: true},
	"mysql.archive.precheck":      {Action: "mysql.archive.precheck", Risk: R0},
	"mysql.archive.start":         {Action: "mysql.archive.start", Risk: R3, ConfirmationRequired: true},
	"mysql.archive.pause":         {Action: "mysql.archive.pause", Risk: R1},
	"mysql.archive.resume":        {Action: "mysql.archive.resume", Risk: R1},
	"mysql.archive.stop":          {Action: "mysql.archive.stop", Risk: R3, ConfirmationRequired: true},
	"oracle.dataguard.status":     {Action: "oracle.dataguard.status", Risk: R0},
	"oracle.status":               {Action: "oracle.status", Risk: R0},
	"oracle.tablespace.list":      {Action: "oracle.tablespace.list", Risk: R0},
	"oracle.datafile.list":        {Action: "oracle.datafile.list", Risk: R0},
	"oracle.datafile.add":         {Action: "oracle.datafile.add", Risk: R3, ConfirmationRequired: true},
	"oracle.datafile.resize":      {Action: "oracle.datafile.resize", Risk: R3, ConfirmationRequired: true},
	"oracle.rman.backup":          {Action: "oracle.rman.backup", Risk: R1},
	"postgres.status":             {Action: "postgres.status", Risk: R0},
	"postgres.replication.status": {Action: "postgres.replication.status", Risk: R0},
	"postgres.backup":             {Action: "postgres.backup", Risk: R1},
	"doris.cluster.status":        {Action: "doris.cluster.status", Risk: R0},
	"doris.backup":                {Action: "doris.backup", Risk: R1},
}

func Get(action string) (Policy, bool) {
	p, ok := policies[strings.TrimSpace(action)]
	return p, ok
}

func Validate(action string, confirmed bool) (Policy, error) {
	p, ok := Get(action)
	if !ok {
		return Policy{}, fmt.Errorf("unknown action policy %q", action)
	}
	if p.ConfirmationRequired && !confirmed {
		return p, fmt.Errorf("%s (%s) requires explicit confirmation", action, p.Risk)
	}
	return p, nil
}
