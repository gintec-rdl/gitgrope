# gitgrope
Serverside GitHub deployment manager

### What

The goal of `gitgrope` is to provide the simplest way known to mankind of getting your repository onto your server, without too many complexities. 
It works by polling your GitHub repositories for latest releaases (via tags) and downloading the releases to the server. 

That's that. The rest is up to you.

### How
1. Create an access token for your private epository. If your repository is public there is no need for an access token.
2. Install, configure, and run `gitgrope` on your server as a service.
3. Expect your releases to be deployed after the amount of polling time configured.
