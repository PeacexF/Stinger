# Disclaimer

SMTP-Stinger ("the software") is provided for **legitimate list hygiene and educational purposes only**. By using it you agree to the terms below.

## Intended use

The software is intended to help you keep **your own** contact data accurate, for example:

- cleaning a list of customers or subscribers who gave you their address
- validating sign-up or CRM data you are lawfully entitled to hold
- testing mail infrastructure you own or are authorised to test

It is **not** intended for, and must not be used for:

- sending unsolicited bulk email (spam), or building or validating lists for that purpose
- harvesting, scraping or enumerating addresses you have no lawful basis to process
- probing mail servers without authorisation, or in violation of their terms or rate limits
- evading blocklists, abuse filters or other anti-abuse measures
- any activity that is unlawful where you, the mail server, or the address owner are located

## Your responsibility

You are solely responsible for how you use the software and for the data you process with it. This includes compliance with:

- data-protection and privacy law, such as the GDPR, UK GDPR, CCPA/CPRA and similar laws
- anti-spam and electronic marketing law, such as CAN-SPAM, PECR, CASL and the ePrivacy Directive
- computer misuse and unauthorised-access law
- the acceptable use policies of your hosting provider, network provider and DNS provider
- the terms of service of any mail provider whose servers you connect to

Nothing in this repository is legal advice. If you are unsure whether your use is lawful, consult a qualified lawyer before running the software.

## Risks

- **IP and domain reputation.** SMTP verification can get your IP address or domain rate-limited, blocklisted or reported for abuse. Your hosting provider may suspend your server.
- **Accuracy is not guaranteed.** Mail servers can accept addresses that don't exist (catch-all domains), reject addresses that do exist (anti-enumeration and greylisting), or behave differently over time. A `valid` result does not guarantee that a message will be delivered. An `invalid` result does not guarantee that the mailbox doesn't exist.
- **Third-party systems.** The software connects to servers you do not control. Their behaviour, availability and policies are outside the authors' control.

## No warranty and no liability

The software is provided **"as is", without warranty of any kind**, as stated in the [MIT License](LICENSE). The authors and contributors are not liable for any claim, damages or other liability arising from use or misuse of the software. This includes legal action, fines, blocklisting, account suspension, data loss and lost business.

The authors do not endorse, and are not responsible for, any use of the software that violates this disclaimer or any applicable law.

## Trademarks

Names such as Gmail, Hotmail, Yahoo, AWS, GCP and Azure are used in the documentation for identification only. They are trademarks of their respective owners. This project is not affiliated with, endorsed by or sponsored by any of them.
