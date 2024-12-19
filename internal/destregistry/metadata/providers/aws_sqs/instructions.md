# AWS SQS Setup Instructions

Basic setup instructions for aws sqs destination.

## Using AWS CLI

(optional) Create a queue if one doesn't exist

```sh
$ aws sqs create-queue --queue-name QUEUENAME --region REGION
```

Create a policy with necessary permissions

```sh
$ aws iam create-policy --policy-name POLICYNAME --policy-document '{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sqs:SendMessage"
      ],
      "Resource": "arn:aws:sqs:REGION:ACCOUNTID:QUEUENAME"
    }
  ]
}'
```

Create a user

```sh
$ aws iam create-user --user-name USERNAME
```

Attach the policy to the user

```sh
$ aws iam attach-user-policy --user-name USERNAME --policy-arn arn:aws:iam::ACCOUNTID:policy/POLICYNAME
```

Create an access key

```sh
$ aws iam create-access-key --user-name USERNAME
```

Delete an access key

```sh
$ aws iam delete-access-key --user-name USERNAME --access-key-id ACCESSKEYID
```
