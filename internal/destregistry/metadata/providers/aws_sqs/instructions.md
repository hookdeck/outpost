# AWS SQS Configuration Instructions

Basic setup instructions for AWS SQS destination.

## Using AWS CLI

1. Create a queue if one doesn't exist (optional)

```sh
$ aws sqs create-queue --queue-name QUEUENAME --region REGION
```

2. Create a policy with necessary permissions

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

3. Create a user

```sh
$ aws iam create-user --user-name USERNAME
```

4. Attach the policy to the user

```sh
$ aws iam attach-user-policy --user-name USERNAME --policy-arn arn:aws:iam::ACCOUNTID:policy/POLICYNAME
```

5. Create an Access Key

```sh
$ aws iam create-access-key --user-name USERNAME
```

6. Configure your AWS SQS Event Destination

Use the Access Key and Access Secret created in step 5 to configure your AWS SQS Event Destination.