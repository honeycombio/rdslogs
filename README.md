Goal for `honeyrds`:
- using the aws CLI, download RDS logs
- launch honeytail on those logs

Suggested method: run once / hour from cron


## To run this thing
launch from somewhere that has IAM roles or .aws/config or .aws/credentials so that it can reach RDS.

```
./honeyrds --identifier production-honeycomb-mysql | honeytail -p mysql -k $kiwi -d rdstail -f -
```


## TODO

write installer script? reuse mysql installer script? set up more clear download instructions so that it only happens after RDS config changes (eg enable slow query log) has happened? aka what's the UX for starting this process all the way through running daemon

