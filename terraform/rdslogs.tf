# This terraform configuration describes the minimum AWS requirements
# for RDSLogs
resource aws_iam_user "honeycomb-rdslogs-svc-user" {
  name = "honeycomb-rdslogs-svc-user"
}

resource "aws_iam_policy" "honeycomb-rdslogs-policy" {
  name = "honeycomb-rdslogs-policy"
  description = "minimal policy for honeycomb RDSLogs"
  policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
    {
        "Effect": "Allow",
        "Action": [
            "rds:DescribeDBInstances",
            "rds:DescribeDBLogFiles",
            "rds:DownloadDBLogFilePortion"
        ],
        "Resource": "*"
    }
  ]
}
EOF
}

resource "aws_iam_user_policy_attachment" "honeycomb-rdslogs-user-policy" {
    user       = "${aws_iam_user.honeycomb-rdslogs-svc-user.name}"
    policy_arn = "${aws_iam_policy.honeycomb-rdslogs-policy.arn}"
}