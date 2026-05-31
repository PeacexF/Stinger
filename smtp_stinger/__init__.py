# Stinger — high-performance CLI SMTP email verifier
#
# Checks emails in bulk
# returns two result files:
# results/
# -> results.jsonl      Full information on each request
# -> valid_emails.txt   Only emails that returned 250 / 251 (catch-all inclusive)
#
# Before running you have to fill in the config at:
# smtp:
# -> helo_hostname: ""      mail.domain.com
# -> mail_from: ""          stinger@domain.com / verufy@domain.com / no-reply@domain.com
# They HAVE to resolve to a valid domain with required records for SMTP


__version__ = "1.0.0"