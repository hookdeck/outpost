# AWS S3 Configuration Instructions

[Amazon Simple Storage Service (S3)](https://aws.amazon.com/s3/) is a highly scalable, reliable, and low-latency data storage infrastructure from Amazon Web Services. S3 provides industry-leading durability, availability, performance, security, and virtually unlimited scalability at very low costs. It provides features such as:

- Multiple storage classes (Standard, Intelligent-Tiering, Glacier, etc.)
- Cross-region replication
- Versioning and lifecycle management
- Event notifications
- Strong consistency
- Server-side encryption

## How to configure AWS S3 as an event destination using the AWS CLI

To follow these steps you will need an AWS account, and the [AWS CLI](https://aws.amazon.com/cli/) installed and authenticated.

1. Create a bucket if one doesn't exist (optional)

    ```sh
    aws s3 mb s3://BUCKETNAME --region REGION
    ```

2. Create a policy with necessary permissions

    ```sh
    aws iam create-policy --policy-name POLICYNAME --policy-document '{
      "Version": "2012-10-17",
      "Statement": [
        {
          "Effect": "Allow",
          "Action": [
            "s3:PutObject"
          ],
          "Resource": "arn:aws:s3:::BUCKETNAME/*"
        }
      ]
    }'
    ```

3. Create a user

    ```sh
    aws iam create-user --user-name USERNAME
    ```

4. Attach the policy to the user

    ```sh
    aws iam attach-user-policy --user-name USERNAME --policy-arn arn:aws:iam::ACCOUNTID:policy/POLICYNAME
    ```

5. Create an Access Key

    ```sh
    aws iam create-access-key --user-name USERNAME
    ```

6. Configure your AWS S3 Event Destination

    Use the Access Key and Secret Access Key created in step 5 to configure your AWS S3 Event Destination.

## Configuration Options

The AWS S3 destination supports the following configuration options:

### Required Configuration

- **Bucket Name**: The name of your S3 bucket where events will be stored
- **AWS Region**: The AWS region where your bucket is located (e.g., `us-east-1`)

### Optional Configuration

- **Object Key Prefix**: Optional prefix to prepend to object keys (e.g., `events/`)
- **Object Key Suffix**: Optional suffix to append to object keys (e.g., `.json`)
- **Include Timestamp**: Whether to include the event timestamp in the object key (default: enabled)
- **Include Event ID**: Whether to include the event ID in the object key (default: enabled)
- **S3 Storage Class**: The storage class for objects (e.g., `STANDARD`, `INTELLIGENT_TIERING`, `GLACIER`)

### Object Key Generation

Object keys are generated using the following pattern:
```
[key_prefix][timestamp][_][event_id][key_suffix]
```

The following examples use "events/" as the prefix and ".json" as the suffix:
- If both timestamp and event ID are enabled: `events/2024-03-20T12:00:00.123456789Z_eee56889-6924-42e6-9d08-6c6b22928fb0.json`
- If only timestamp is enabled: `events/2024-03-20T12:00:00.123456789Z.json`
- If only event ID is enabled: `events/eee56889-6924-42e6-9d08-6c6b22928fb0.json`
- If neither is enabled: `events/.json` (not a good idea, keys will collide)

### Storage Classes

Valid S3 storage classes include:
- `STANDARD` (default)
- `REDUCED_REDUNDANCY`
- `STANDARD_IA`
- `ONEZONE_IA`
- `INTELLIGENT_TIERING`
- `GLACIER`
- `DEEP_ARCHIVE`
- `OUTPOSTS`
- `GLACIER_IR`

## Event Format

Events are stored as JSON objects in S3 with:
- **Content-Type**: `application/json`
- **Body**: The event data as JSON
- **Metadata**: Event metadata is stored as S3 object metadata
- **Checksum**: SHA256 checksum for data integrity

## IAM Permissions

The minimum required IAM permissions for the S3 destination are:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject"
      ],
      "Resource": "arn:aws:s3:::your-bucket-name/*"
    }
  ]
}
```
