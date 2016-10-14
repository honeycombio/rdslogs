Honeyrds is a tool to download or stream RDS
aunch from somewhere that has IAM roles or .aws/config or .aws/credentials so that it can reach RDS.

```
/honeyrds --identifier my-rds-database | honeytail -p mysql -k $kiwi -d rdstail -f -
```

