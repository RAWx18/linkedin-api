# Getting Your Session Values

The web UI's "Use your own LinkedIn session" panel asks for two cookies and,
optionally, your browser's User-Agent.

## li_at and JSESSIONID

1. Open [linkedin.com](https://www.linkedin.com) in your browser and sign in.
2. Right-click the page, choose **Inspect**, and open the **Application** tab.
3. Under **Storage**, expand **Cookies** and select `https://www.linkedin.com`.
4. Copy the values of `li_at` and `JSESSIONID` and paste them into the UI.

![li_at and JSESSIONID in DevTools](public/sessionid.png)

## User-Agent

In the same DevTools window, open the **Console** tab and run:

```js
navigator.userAgent
```

Copy the printed string into the User-Agent field so requests match the browser
that created the cookies.

Treat all three values like passwords. The UI sends them once for the request
and never stores them; see [security.md](security.md) for the guarantees.
