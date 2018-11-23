# selective-site-crawler
### Overview
Crawls on specific pages of a website that meet a dynamic criteria. 

### Build
    $ go build .
### Run
#### Pattern: 
    ./selective-site-crawler -host https://example.com [-pages] [-timeout]

#### Examples:
    # runs for 5 seconds 
    ./selective-site-crawler -host https://example.com -timeout 5
    
    # runs until limit of 2 saved pages  is reached
    ./selective-site-crawler -host https://example.com -pages 2
    
    # Stops when at least one of the 2 constraints is reached
    ./selective-site-crawler -host https://example.com -pages 2 -timeout 5
    
    
### Flags
-h
        
    Prints the details of the flags
    
-host

    Required. The Host to crawl.
    
-timeout

    At least -timeout or -pages is required. The lifetime time in seconds for the script to run 
    
-pages

    At least -timeout or -pages is required. The number of pages to download.# selective-site-crawler
